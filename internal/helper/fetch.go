package helper

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/dlclark/regexp2"
)

func buildUpstreamStatusError(prefix string, statusCode int, bodyPreview []byte) error {
	body := strings.Join(strings.Fields(strings.TrimSpace(string(bodyPreview))), " ")
	if body == "" {
		return fmt.Errorf("%s failed: status %d, empty response body", prefix, statusCode)
	}
	return fmt.Errorf("%s failed: status %d, body: %s", prefix, statusCode, body)
}

func FetchModels(ctx context.Context, request model.Channel) ([]string, error) {
	client, err := ChannelHttpClient(&request)
	if err != nil {
		log.Errorf("fetch models http client init failed, channel=%s, err=%v", request.Name, err)
		return nil, err
	}
	fetchModel := make([]string, 0)
	switch request.Type {
	case outbound.OutboundTypeAnthropic:
		fetchModel, err = fetchAnthropicModels(client, ctx, request)
	case outbound.OutboundTypeGemini:
		fetchModel, err = fetchGeminiModels(client, ctx, request)
	default:
		fetchModel, err = fetchOpenAIModels(client, ctx, request)
	}
	if err != nil {
		log.Errorf("fetch models failed, channel=%s, type=%d, err=%v", request.Name, request.Type, err)
		return nil, err
	}
	if request.MatchRegex != nil && *request.MatchRegex != "" {
		matchModel := make([]string, 0)
		re, err := regexp2.Compile(*request.MatchRegex, regexp2.ECMAScript)
		if err != nil {
			log.Errorf("fetch models regex compile failed, channel=%s, regex=%s, err=%v", request.Name, *request.MatchRegex, err)
			return nil, err
		}
		for _, model := range fetchModel {
			matched, err := re.MatchString(model)
			if err != nil {
				log.Errorf("fetch models regex match failed, channel=%s, model=%s, regex=%s, err=%v", request.Name, model, *request.MatchRegex, err)
				return nil, err
			}
			if matched {
				matchModel = append(matchModel, model)
			}
		}
		return matchModel, nil
	}
	return fetchModel, nil
}

// refer: https://platform.openai.com/docs/api-reference/models/list
func fetchOpenAIModels(client *http.Client, ctx context.Context, request model.Channel) ([]string, error) {
	baseURL := request.GetBaseUrl()
	if baseURL == "" {
		err := errors.New("fetch openai models failed: base url is empty")
		log.Errorf("%v, channel=%s", err, request.Name)
		return nil, err
	}

	channelKey := request.GetChannelKey().ChannelKey
	if channelKey == "" {
		err := errors.New("fetch openai models failed: channel key is empty")
		log.Errorf("%v, channel=%s", err, request.Name)
		return nil, err
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		baseURL+"/models",
		nil,
	)
	if err != nil {
		log.Errorf("fetch openai models request build failed, channel=%s, base_url=%s, err=%v", request.Name, baseURL, err)
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+channelKey)
	for _, header := range request.CustomHeader {
		headerKey := strings.TrimSpace(header.HeaderKey)
		if headerKey == "" {
			continue
		}
		if header.HeaderValue == "" {
			req.Header.Del(headerKey)
			continue
		}
		req.Header.Set(headerKey, header.HeaderValue)
	}

	resp, err := client.Do(req)
	if err != nil {
		log.Errorf("fetch openai models request failed, channel=%s, base_url=%s, err=%v", request.Name, baseURL, err)
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusBadRequest {
		bodyPreview, readErr := io.ReadAll(io.LimitReader(resp.Body, 1024))
		if readErr != nil {
			log.Errorf("fetch openai models failed, channel=%s, base_url=%s, status=%d, body_read_err=%v", request.Name, baseURL, resp.StatusCode, readErr)
			return nil, fmt.Errorf("fetch openai models failed: status %d (failed to read upstream error body: %w)", resp.StatusCode, readErr)
		}
		log.Errorf("fetch openai models failed, channel=%s, base_url=%s, status=%d, body=%s", request.Name, baseURL, resp.StatusCode, strings.TrimSpace(string(bodyPreview)))
		return nil, buildUpstreamStatusError("fetch openai models", resp.StatusCode, bodyPreview)
	}

	var result model.OpenAIModelList
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		log.Errorf("fetch openai models decode failed, channel=%s, base_url=%s, err=%v", request.Name, baseURL, err)
		return nil, err
	}
	if len(result.Data) == 0 {
		err := errors.New("fetch openai models returned empty data")
		log.Errorf("%v, channel=%s, base_url=%s", err, request.Name, baseURL)
		return nil, err
	}

	models := make([]string, 0, len(result.Data))
	for _, m := range result.Data {
		if m.ID != "" {
			models = append(models, m.ID)
		}
	}
	if len(models) == 0 {
		err := errors.New("fetch openai models returned no valid model id")
		log.Errorf("%v, channel=%s, base_url=%s", err, request.Name, baseURL)
		return nil, err
	}
	return models, nil
}

// refer: https://ai.google.dev/api/models
func fetchGeminiModels(client *http.Client, ctx context.Context, request model.Channel) ([]string, error) {
	baseURL := request.GetBaseUrl()
	if baseURL == "" {
		err := errors.New("fetch gemini models failed: base url is empty")
		log.Errorf("%v, channel=%s", err, request.Name)
		return nil, err
	}

	channelKey := request.GetChannelKey().ChannelKey
	if channelKey == "" {
		err := errors.New("fetch gemini models failed: channel key is empty")
		log.Errorf("%v, channel=%s", err, request.Name)
		return nil, err
	}

	var allModels []string
	pageToken := ""

	for {
		req, err := http.NewRequestWithContext(
			ctx,
			http.MethodGet,
			baseURL+"/models",
			nil,
		)
		if err != nil {
			log.Errorf("fetch gemini models request build failed, channel=%s, base_url=%s, err=%v", request.Name, baseURL, err)
			return nil, err
		}

		req.Header.Set("X-Goog-Api-Key", channelKey)
		for _, header := range request.CustomHeader {
			headerKey := strings.TrimSpace(header.HeaderKey)
			if headerKey == "" {
				continue
			}
			if header.HeaderValue == "" {
				req.Header.Del(headerKey)
				continue
			}
			req.Header.Set(headerKey, header.HeaderValue)
		}
		if pageToken != "" {
			q := req.URL.Query()
			q.Add("pageToken", pageToken)
			req.URL.RawQuery = q.Encode()
		}

		resp, err := client.Do(req)
		if err != nil {
			log.Errorf("fetch gemini models request failed, channel=%s, base_url=%s, page_token=%s, err=%v", request.Name, baseURL, pageToken, err)
			return nil, err
		}
		if resp.StatusCode >= http.StatusBadRequest {
			bodyPreview, readErr := io.ReadAll(io.LimitReader(resp.Body, 1024))
			resp.Body.Close()
			if readErr != nil {
				log.Errorf("fetch gemini models failed, channel=%s, base_url=%s, page_token=%s, status=%d, body_read_err=%v", request.Name, baseURL, pageToken, resp.StatusCode, readErr)
				return nil, fmt.Errorf("fetch gemini models failed: status %d (failed to read upstream error body: %w)", resp.StatusCode, readErr)
			}
			log.Errorf("fetch gemini models failed, channel=%s, base_url=%s, page_token=%s, status=%d, body=%s", request.Name, baseURL, pageToken, resp.StatusCode, strings.TrimSpace(string(bodyPreview)))
			return nil, buildUpstreamStatusError("fetch gemini models", resp.StatusCode, bodyPreview)
		}

		var result model.GeminiModelList
		err = json.NewDecoder(resp.Body).Decode(&result)
		resp.Body.Close()
		if err != nil {
			log.Errorf("fetch gemini models decode failed, channel=%s, base_url=%s, page_token=%s, err=%v", request.Name, baseURL, pageToken, err)
			return nil, err
		}

		for _, m := range result.Models {
			name := strings.TrimPrefix(m.Name, "models/")
			allModels = append(allModels, name)
		}

		if result.NextPageToken == "" {
			break
		}
		pageToken = result.NextPageToken
	}
	if len(allModels) == 0 {
		return fetchOpenAIModels(client, ctx, request)
	}
	return allModels, nil
}

// refer: https://platform.claude.com/docs
func fetchAnthropicModels(client *http.Client, ctx context.Context, request model.Channel) ([]string, error) {
	baseURL := request.GetBaseUrl()
	if baseURL == "" {
		err := errors.New("fetch anthropic models failed: base url is empty")
		log.Errorf("%v, channel=%s", err, request.Name)
		return nil, err
	}

	channelKey := request.GetChannelKey().ChannelKey
	if channelKey == "" {
		err := errors.New("fetch anthropic models failed: channel key is empty")
		log.Errorf("%v, channel=%s", err, request.Name)
		return nil, err
	}

	var allModels []string
	var afterID string
	for {
		req, err := http.NewRequestWithContext(
			ctx,
			http.MethodGet,
			baseURL+"/models",
			nil,
		)
		if err != nil {
			log.Errorf("fetch anthropic models request build failed, channel=%s, base_url=%s, err=%v", request.Name, baseURL, err)
			return nil, err
		}

		req.Header.Set("X-Api-Key", channelKey)
		req.Header.Set("Anthropic-Version", "2023-06-01")
		for _, header := range request.CustomHeader {
			headerKey := strings.TrimSpace(header.HeaderKey)
			if headerKey == "" {
				continue
			}
			if header.HeaderValue == "" {
				req.Header.Del(headerKey)
				continue
			}
			req.Header.Set(headerKey, header.HeaderValue)
		}
		q := req.URL.Query()
		if afterID != "" {
			q.Set("after_id", afterID)
		}
		req.URL.RawQuery = q.Encode()

		resp, err := client.Do(req)
		if err != nil {
			log.Errorf("fetch anthropic models request failed, channel=%s, base_url=%s, after_id=%s, err=%v", request.Name, baseURL, afterID, err)
			return nil, err
		}
		if resp.StatusCode >= http.StatusBadRequest {
			bodyPreview, readErr := io.ReadAll(io.LimitReader(resp.Body, 1024))
			resp.Body.Close()
			if readErr != nil {
				log.Errorf("fetch anthropic models failed, channel=%s, base_url=%s, after_id=%s, status=%d, body_read_err=%v", request.Name, baseURL, afterID, resp.StatusCode, readErr)
				return nil, fmt.Errorf("fetch anthropic models failed: status %d (failed to read upstream error body: %w)", resp.StatusCode, readErr)
			}
			log.Errorf("fetch anthropic models failed, channel=%s, base_url=%s, after_id=%s, status=%d, body=%s", request.Name, baseURL, afterID, resp.StatusCode, strings.TrimSpace(string(bodyPreview)))
			return nil, buildUpstreamStatusError("fetch anthropic models", resp.StatusCode, bodyPreview)
		}

		var result model.AnthropicModelList
		err = json.NewDecoder(resp.Body).Decode(&result)
		resp.Body.Close()
		if err != nil {
			log.Errorf("fetch anthropic models decode failed, channel=%s, base_url=%s, after_id=%s, err=%v", request.Name, baseURL, afterID, err)
			return nil, err
		}

		for _, m := range result.Data {
			allModels = append(allModels, m.ID)
		}

		if !result.HasMore {
			break
		}

		afterID = result.LastID
	}
	if len(allModels) == 0 {
		return fetchOpenAIModels(client, ctx, request)
	}
	return allModels, nil
}
