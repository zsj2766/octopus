package relay

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/helper"
	dbmodel "github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/relay/balancer"
	"github.com/bestruirui/octopus/internal/server/resp"
	"github.com/bestruirui/octopus/internal/transformer/inbound"
	"github.com/bestruirui/octopus/internal/transformer/model"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/gin-gonic/gin"
	"github.com/tmaxmax/go-sse"
)

// Handler 处理入站请求并转发到上游服务
func Handler(inboundType inbound.InboundType, c *gin.Context) {
	// 解析请求
	internalRequest, inAdapter, err := parseRequest(inboundType, c)
	if err != nil {
		return
	}
	supportedModels := c.GetString("supported_models")
	if supportedModels != "" {
		supportedModelsArray := strings.Split(supportedModels, ",")
		if !slices.Contains(supportedModelsArray, internalRequest.Model) {
			resp.Error(c, http.StatusBadRequest, "model not supported")
			return
		}
	}

	requestModel := internalRequest.Model
	apiKeyID := c.GetInt("api_key_id")

	// 获取通道分组
	group, err := op.GroupGetMap(requestModel, c.Request.Context())
	if err != nil {
		resp.Error(c, http.StatusNotFound, "model not found")
		return
	}

	// 创建迭代器（策略排序 + 粘性优先）
	iter := balancer.NewIterator(group, apiKeyID, requestModel)
	if iter.Len() == 0 {
		resp.Error(c, http.StatusServiceUnavailable, "no available channel")
		return
	}

	// 初始化 Metrics
	metrics := NewRelayMetrics(apiKeyID, requestModel, internalRequest)

	// 请求级上下文
	req := &relayRequest{
		c:               c,
		inAdapter:       inAdapter,
		internalRequest: internalRequest,
		metrics:         metrics,
		apiKeyID:        apiKeyID,
		requestModel:    requestModel,
		iter:            iter,
	}

	var lastErr error

	for iter.Next() {
		select {
		case <-c.Request.Context().Done():
			log.Infof("request context canceled, stopping retry")
			metrics.Save(c.Request.Context(), false, context.Canceled, iter.Attempts())
			return
		default:
		}

		item := iter.Item()

		// 获取通道
		channel, err := op.ChannelGet(item.ChannelID, c.Request.Context())
		if err != nil {
			log.Warnf("failed to get channel %d: %v", item.ChannelID, err)
			iter.Skip(item.ChannelID, 0, fmt.Sprintf("channel_%d", item.ChannelID), fmt.Sprintf("channel not found: %v", err))
			lastErr = err
			continue
		}
		if !channel.Enabled {
			iter.Skip(channel.ID, 0, channel.Name, "channel disabled")
			continue
		}

		usedKey := channel.GetChannelKey()
		if usedKey.ChannelKey == "" {
			iter.Skip(channel.ID, 0, channel.Name, "no available key")
			continue
		}

		// 熔断检查
		if iter.SkipCircuitBreak(channel.ID, usedKey.ID, channel.Name) {
			continue
		}

		// 出站适配器
		outAdapter := outbound.Get(channel.Type)
		if outAdapter == nil {
			iter.Skip(channel.ID, usedKey.ID, channel.Name, fmt.Sprintf("unsupported channel type: %d", channel.Type))
			continue
		}

		// 类型兼容性检查
		if internalRequest.IsEmbeddingRequest() && !outbound.IsEmbeddingChannelType(channel.Type) {
			iter.Skip(channel.ID, usedKey.ID, channel.Name, "channel type not compatible with embedding request")
			continue
		}
		if internalRequest.IsChatRequest() && !outbound.IsChatChannelType(channel.Type) {
			iter.Skip(channel.ID, usedKey.ID, channel.Name, "channel type not compatible with chat request")
			continue
		}

		// 设置实际模型
		internalRequest.Model = item.ModelName

		log.Infof("request model %s, mode: %d, forwarding to channel: %s model: %s (attempt %d/%d, sticky=%t)",
			requestModel, group.Mode, channel.Name, item.ModelName,
			iter.Index()+1, iter.Len(), iter.IsSticky())

		// 构造尝试级上下文 -- 只写变化的 4 个字段
		ra := &relayAttempt{
			relayRequest:         req,
			outAdapter:           outAdapter,
			channel:              channel,
			usedKey:              usedKey,
			firstTokenTimeOutSec: group.FirstTokenTimeOut,
		}

		result := ra.attempt()
		if result.Success {
			metrics.Save(c.Request.Context(), true, nil, iter.Attempts())
			return
		}
		if result.Written {
			metrics.Save(c.Request.Context(), false, result.Err, iter.Attempts())
			return
		}
		lastErr = result.Err
	}

	// 所有通道都失败
	metrics.Save(c.Request.Context(), false, lastErr, iter.Attempts())
	resp.Error(c, http.StatusBadGateway, "all channels failed")
}

// attempt 统一管理一次通道尝试的完整生命周期
func (ra *relayAttempt) attempt() attemptResult {
	span := ra.iter.StartAttempt(ra.channel.ID, ra.usedKey.ID, ra.channel.Name)

	span.SetModelName(ra.internalRequest.Model)

	// 转发请求
	statusCode, fwdErr := ra.forward(span)

	// 更新 channel key 状态
	ra.usedKey.StatusCode = statusCode
	ra.usedKey.LastUseTimeStamp = time.Now().Unix()

	if fwdErr == nil {
		// ====== 成功 ======
		ra.collectResponse()
		ra.usedKey.TotalCost += ra.metrics.Stats.InputCost + ra.metrics.Stats.OutputCost
		op.ChannelKeyUpdate(ra.usedKey)

		span.End(dbmodel.AttemptSuccess, statusCode, "")

		// Channel 维度统计
		op.StatsChannelUpdate(ra.channel.ID, dbmodel.StatsMetrics{
			WaitTime:       span.Duration().Milliseconds(),
			RequestSuccess: 1,
		})

		// 熔断器：记录成功
		balancer.RecordSuccess(ra.channel.ID, ra.usedKey.ID, ra.metrics.InternalRequest.Model)
		// 会话保持：更新粘性记录
		balancer.SetSticky(ra.apiKeyID, ra.requestModel, ra.channel.ID, ra.usedKey.ID)

		return attemptResult{Success: true}
	}

	// ====== 失败 ======
	op.ChannelKeyUpdate(ra.usedKey)
	span.End(dbmodel.AttemptFailed, statusCode, fwdErr.Error())

	// Channel 维度统计
	op.StatsChannelUpdate(ra.channel.ID, dbmodel.StatsMetrics{
		WaitTime:      span.Duration().Milliseconds(),
		RequestFailed: 1,
	})

	// 熔断器：记录失败
	balancer.RecordFailure(ra.channel.ID, ra.usedKey.ID, ra.metrics.InternalRequest.Model)

	written := ra.c.Writer.Written()
	if written {
		ra.collectResponse()
	}
	return attemptResult{
		Success: false,
		Written: written,
		Err:     fmt.Errorf("channel %s failed: %v", ra.channel.Name, fwdErr),
	}
}

// parseRequest 解析并验证入站请求
func parseRequest(inboundType inbound.InboundType, c *gin.Context) (*model.InternalLLMRequest, model.Inbound, error) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return nil, nil, err
	}

	inAdapter := inbound.Get(inboundType)
	internalRequest, err := inAdapter.TransformRequest(c.Request.Context(), body)
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return nil, nil, err
	}

	// Pass through the original query parameters
	internalRequest.Query = c.Request.URL.Query()

	if err := internalRequest.Validate(); err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return nil, nil, err
	}

	return internalRequest, inAdapter, nil
}

// forward 转发请求到上游服务
func (ra *relayAttempt) forward(span *balancer.AttemptSpan) (int, error) {
	ctx := ra.c.Request.Context()

	outboundInternalRequest, err := ra.buildOutboundInternalRequest()
	if err != nil {
		log.Warnf("failed to apply request override: %v", err)
		return 0, fmt.Errorf("failed to apply request override: %w", err)
	}
	span.SetModelName(outboundInternalRequest.Model)
	span.SetRequestDiff(buildRequestDiff(ra.internalRequest, outboundInternalRequest))
	// 记录本次尝试实际发送的请求体
	ra.metrics.InternalRequest = outboundInternalRequest

	// 构建出站请求
	outboundRequest, err := ra.outAdapter.TransformRequest(
		ctx,
		outboundInternalRequest,
		ra.channel.GetBaseUrl(),
		ra.usedKey.ChannelKey,
	)
	if err != nil {
		log.Warnf("failed to create request: %v", err)
		return 0, fmt.Errorf("failed to create request: %w", err)
	}

	// 复制请求头
	span.SetHeaderDiff(ra.copyHeaders(outboundRequest))

	// 发送请求
	response, err := ra.sendRequest(outboundRequest)
	if err != nil {
		return 0, fmt.Errorf("failed to send request: %w", err)
	}
	defer response.Body.Close()

	// 检查响应状态
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, err := io.ReadAll(response.Body)
		if err != nil {
			return 0, fmt.Errorf("failed to read response body: %w", err)
		}
		return 0, fmt.Errorf("upstream error: %d: %s", response.StatusCode, string(body))
	}

	// 处理响应
	if outboundInternalRequest.Stream != nil && *outboundInternalRequest.Stream {
		if err := ra.handleStreamResponse(ctx, response); err != nil {
			return 0, err
		}
		return response.StatusCode, nil
	}
	if err := ra.handleResponse(ctx, response); err != nil {
		return 0, err
	}
	return response.StatusCode, nil
}

// copyHeaders 复制请求头，过滤 hop-by-hop 头
func (ra *relayAttempt) copyHeaders(outboundRequest *http.Request) []dbmodel.HeaderDiffItem {
	for key, values := range ra.c.Request.Header {
		if hopByHopHeaders[strings.ToLower(key)] {
			continue
		}
		for _, value := range values {
			outboundRequest.Header.Set(key, value)
		}
	}

	beforeCustom := cloneHeaderMap(outboundRequest.Header)
	if len(ra.channel.CustomHeader) == 0 {
		return nil
	}

	for _, header := range ra.channel.CustomHeader {
		headerKey := strings.TrimSpace(header.HeaderKey)
		if headerKey == "" {
			continue
		}
		if header.HeaderValue == "" {
			outboundRequest.Header.Del(headerKey)
			continue
		}
		outboundRequest.Header.Set(headerKey, header.HeaderValue)
	}
	return buildHeaderDiff(beforeCustom, outboundRequest.Header)
}

// sendRequest 发送 HTTP 请求
func (ra *relayAttempt) sendRequest(req *http.Request) (*http.Response, error) {
	httpClient, err := helper.ChannelHttpClient(ra.channel)
	if err != nil {
		log.Warnf("failed to get http client: %v", err)
		return nil, err
	}

	response, err := httpClient.Do(req)
	if err != nil {
		log.Warnf("failed to send request: %v", err)
		return nil, err
	}

	return response, nil
}

// handleStreamResponse 处理流式响应
func (ra *relayAttempt) handleStreamResponse(ctx context.Context, response *http.Response) error {
	if ct := response.Header.Get("Content-Type"); ct != "" && !strings.Contains(strings.ToLower(ct), "text/event-stream") {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 16*1024))
		return fmt.Errorf("upstream returned non-SSE content-type %q for stream request: %s", ct, string(body))
	}

	// 设置 SSE 响应头
	ra.c.Header("Content-Type", "text/event-stream")
	ra.c.Header("Cache-Control", "no-cache")
	ra.c.Header("Connection", "keep-alive")
	ra.c.Header("X-Accel-Buffering", "no")

	firstToken := true

	type sseReadResult struct {
		data string
		err  error
	}
	results := make(chan sseReadResult, 1)
	go func() {
		defer close(results)
		readCfg := &sse.ReadConfig{MaxEventSize: maxSSEEventSize}
		for ev, err := range sse.Read(response.Body, readCfg) {
			if err != nil {
				results <- sseReadResult{err: err}
				return
			}
			results <- sseReadResult{data: ev.Data}
		}
	}()

	var firstTokenTimer *time.Timer
	var firstTokenC <-chan time.Time
	if firstToken && ra.firstTokenTimeOutSec > 0 {
		firstTokenTimer = time.NewTimer(time.Duration(ra.firstTokenTimeOutSec) * time.Second)
		firstTokenC = firstTokenTimer.C
		defer func() {
			if firstTokenTimer != nil {
				firstTokenTimer.Stop()
			}
		}()
	}

	for {
		select {
		case <-ctx.Done():
			log.Infof("client disconnected, stopping stream")
			return nil
		case <-firstTokenC:
			log.Warnf("first token timeout (%ds), switching channel", ra.firstTokenTimeOutSec)
			_ = response.Body.Close()
			return fmt.Errorf("first token timeout (%ds)", ra.firstTokenTimeOutSec)
		case r, ok := <-results:
			if !ok {
				log.Infof("stream end")
				return nil
			}
			if r.err != nil {
				log.Warnf("failed to read event: %v", r.err)
				return fmt.Errorf("failed to read stream event: %w", r.err)
			}

			data, err := ra.transformStreamData(ctx, r.data)
			if err != nil || len(data) == 0 {
				continue
			}
			if firstToken {
				ra.metrics.SetFirstTokenTime(time.Now())
				firstToken = false
				if firstTokenTimer != nil {
					if !firstTokenTimer.Stop() {
						select {
						case <-firstTokenTimer.C:
						default:
						}
					}
					firstTokenTimer = nil
					firstTokenC = nil
				}
			}

			ra.c.Writer.Write(data)
			ra.c.Writer.Flush()
		}
	}
}

// transformStreamData 转换流式数据
func (ra *relayAttempt) transformStreamData(ctx context.Context, data string) ([]byte, error) {
	internalStream, err := ra.outAdapter.TransformStream(ctx, []byte(data))
	if err != nil {
		log.Warnf("failed to transform stream: %v", err)
		return nil, err
	}
	if internalStream == nil {
		return nil, nil
	}

	inStream, err := ra.inAdapter.TransformStream(ctx, internalStream)
	if err != nil {
		log.Warnf("failed to transform stream: %v", err)
		return nil, err
	}

	return inStream, nil
}

// handleResponse 处理非流式响应
func (ra *relayAttempt) handleResponse(ctx context.Context, response *http.Response) error {
	internalResponse, err := ra.outAdapter.TransformResponse(ctx, response)
	if err != nil {
		log.Warnf("failed to transform response: %v", err)
		return fmt.Errorf("failed to transform outbound response: %w", err)
	}

	inResponse, err := ra.inAdapter.TransformResponse(ctx, internalResponse)
	if err != nil {
		log.Warnf("failed to transform response: %v", err)
		return fmt.Errorf("failed to transform inbound response: %w", err)
	}

	ra.c.Data(http.StatusOK, "application/json", inResponse)
	return nil
}

// collectResponse 收集响应信息
func (ra *relayAttempt) collectResponse() {
	internalResponse, err := ra.inAdapter.GetInternalResponse(ra.c.Request.Context())
	if err != nil || internalResponse == nil {
		return
	}

	ra.metrics.SetInternalResponse(internalResponse, ra.metrics.InternalRequest.Model)
}

func (ra *relayAttempt) buildOutboundInternalRequest() (*model.InternalLLMRequest, error) {
	base, err := deepCopyInternalRequest(ra.internalRequest)
	if err != nil {
		return nil, err
	}
	if ra.channel.ParamOverride == nil {
		return base, nil
	}

	override := strings.TrimSpace(*ra.channel.ParamOverride)
	if override == "" {
		return base, nil
	}

	patched, err := applyJSONMergePatch(base, []byte(override))
	if err != nil {
		return nil, err
	}
	if err := patched.Validate(); err != nil {
		return nil, fmt.Errorf("invalid request after param override: %w", err)
	}
	return patched, nil
}

func deepCopyInternalRequest(in *model.InternalLLMRequest) (*model.InternalLLMRequest, error) {
	if in == nil {
		return nil, fmt.Errorf("internal request is nil")
	}
	b, err := json.Marshal(in)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal internal request: %w", err)
	}
	var out model.InternalLLMRequest
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("failed to unmarshal internal request: %w", err)
	}

	out.RawRequest = in.RawRequest
	out.RawAPIFormat = in.RawAPIFormat
	out.TransformerMetadata = in.TransformerMetadata
	out.TransformOptions = in.TransformOptions
	out.Query = in.Query
	out.Include = in.Include
	if in.ExtraBody != nil {
		out.ExtraBody = append([]byte(nil), in.ExtraBody...)
	}
	return &out, nil
}

func applyJSONMergePatch(base *model.InternalLLMRequest, patch []byte) (*model.InternalLLMRequest, error) {
	baseBytes, err := json.Marshal(base)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal base request: %w", err)
	}

	if !json.Valid(patch) {
		return nil, fmt.Errorf("param_override must be valid json")
	}

	var patchValue any
	if err := json.Unmarshal(patch, &patchValue); err != nil {
		return nil, fmt.Errorf("param_override must be a json object: %w", err)
	}
	patchObject, ok := patchValue.(map[string]any)
	if !ok || patchObject == nil {
		return nil, fmt.Errorf("param_override must be a json object")
	}

	var baseObject map[string]any
	if err := json.Unmarshal(baseBytes, &baseObject); err != nil {
		return nil, fmt.Errorf("failed to unmarshal base request object: %w", err)
	}

	mergedObject := mergePatchObject(baseObject, patchObject)
	merged, err := json.Marshal(mergedObject)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal merged request: %w", err)
	}

	var out model.InternalLLMRequest
	if err := json.Unmarshal(merged, &out); err != nil {
		return nil, fmt.Errorf("failed to unmarshal merged request: %w", err)
	}

	out.RawRequest = base.RawRequest
	out.RawAPIFormat = base.RawAPIFormat
	out.TransformerMetadata = base.TransformerMetadata
	out.TransformOptions = base.TransformOptions
	out.Query = base.Query
	out.Include = base.Include
	if base.ExtraBody != nil {
		out.ExtraBody = append([]byte(nil), base.ExtraBody...)
	}

	return &out, nil
}

func mergePatchObject(base map[string]any, patch map[string]any) map[string]any {
	if base == nil {
		base = map[string]any{}
	}
	for key, patchValue := range patch {
		if patchValue == nil {
			delete(base, key)
			continue
		}

		if patchMap, ok := patchValue.(map[string]any); ok {
			if baseMap, ok := base[key].(map[string]any); ok {
				base[key] = mergePatchObject(baseMap, patchMap)
			} else {
				base[key] = mergePatchObject(map[string]any{}, patchMap)
			}
			continue
		}

		base[key] = patchValue
	}
	return base
}

func buildRequestDiff(beforeReq, afterReq *model.InternalLLMRequest) []dbmodel.RequestDiffItem {
	if beforeReq == nil || afterReq == nil {
		return nil
	}
	beforeMap := internalRequestToMap(beforeReq)
	afterMap := internalRequestToMap(afterReq)
	if beforeMap == nil || afterMap == nil {
		return nil
	}
	var diffs []dbmodel.RequestDiffItem
	buildJSONDiff("", beforeMap, afterMap, &diffs)
	return diffs
}

func internalRequestToMap(req *model.InternalLLMRequest) map[string]any {
	b, err := json.Marshal(req)
	if err != nil {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		return nil
	}
	return out
}

func buildJSONDiff(path string, before, after any, diffs *[]dbmodel.RequestDiffItem) {
	if reflect.DeepEqual(before, after) {
		return
	}

	beforeMap, beforeIsMap := before.(map[string]any)
	afterMap, afterIsMap := after.(map[string]any)
	if beforeIsMap && afterIsMap {
		keySet := make(map[string]struct{}, len(beforeMap)+len(afterMap))
		for k := range beforeMap {
			keySet[k] = struct{}{}
		}
		for k := range afterMap {
			keySet[k] = struct{}{}
		}
		keys := make([]string, 0, len(keySet))
		for k := range keySet {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			nextPath := "/" + k
			if path != "" {
				nextPath = path + "/" + k
			}
			beforeValue, beforeOK := beforeMap[k]
			afterValue, afterOK := afterMap[k]
			switch {
			case !beforeOK && afterOK:
				*diffs = append(*diffs, dbmodel.RequestDiffItem{
					Path:      nextPath,
					Operation: dbmodel.DiffOperationAdd,
					After:     cloneJSONLike(afterValue),
				})
			case beforeOK && !afterOK:
				*diffs = append(*diffs, dbmodel.RequestDiffItem{
					Path:      nextPath,
					Operation: dbmodel.DiffOperationRemove,
					Before:    cloneJSONLike(beforeValue),
				})
			default:
				buildJSONDiff(nextPath, beforeValue, afterValue, diffs)
			}
		}
		return
	}

	op := dbmodel.DiffOperationReplace
	if before == nil && after != nil {
		op = dbmodel.DiffOperationAdd
	} else if before != nil && after == nil {
		op = dbmodel.DiffOperationRemove
	}
	item := dbmodel.RequestDiffItem{
		Path:      path,
		Operation: op,
	}
	if op != dbmodel.DiffOperationAdd {
		item.Before = cloneJSONLike(before)
	}
	if op != dbmodel.DiffOperationRemove {
		item.After = cloneJSONLike(after)
	}
	*diffs = append(*diffs, item)
}

func cloneJSONLike(v any) any {
	if v == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return v
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		return v
	}
	return out
}

func cloneHeaderMap(header http.Header) map[string][]string {
	out := make(map[string][]string, len(header))
	for key, values := range header {
		if len(values) == 0 {
			out[key] = nil
			continue
		}
		cloned := append([]string(nil), values...)
		out[key] = cloned
	}
	return out
}

func buildHeaderDiff(before, after http.Header) []dbmodel.HeaderDiffItem {
	if before == nil {
		before = http.Header{}
	}
	if after == nil {
		after = http.Header{}
	}
	keys := make(map[string]struct{}, len(before)+len(after))
	for k := range before {
		keys[k] = struct{}{}
	}
	for k := range after {
		keys[k] = struct{}{}
	}

	diffs := make([]dbmodel.HeaderDiffItem, 0)
	orderedKeys := make([]string, 0, len(keys))
	for key := range keys {
		orderedKeys = append(orderedKeys, key)
	}
	sort.Strings(orderedKeys)
	for _, key := range orderedKeys {
		beforeValues, beforeOK := before[key]
		afterValues, afterOK := after[key]
		switch {
		case !beforeOK && afterOK:
			diffs = append(diffs, dbmodel.HeaderDiffItem{
				HeaderKey: key,
				Operation: dbmodel.DiffOperationAdd,
				After:     append([]string(nil), afterValues...),
			})
		case beforeOK && !afterOK:
			diffs = append(diffs, dbmodel.HeaderDiffItem{
				HeaderKey: key,
				Operation: dbmodel.DiffOperationRemove,
				Before:    append([]string(nil), beforeValues...),
			})
		default:
			if reflect.DeepEqual(beforeValues, afterValues) {
				continue
			}
			diffs = append(diffs, dbmodel.HeaderDiffItem{
				HeaderKey: key,
				Operation: dbmodel.DiffOperationReplace,
				Before:    append([]string(nil), beforeValues...),
				After:     append([]string(nil), afterValues...),
			})
		}
	}
	return diffs
}
