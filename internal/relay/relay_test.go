package relay

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	dbmodel "github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/transformer/model"
	"github.com/gin-gonic/gin"
)

func TestBuildOutboundInternalRequest_NoOverride(t *testing.T) {
	base := &model.InternalLLMRequest{
		Model: "gpt-4o-mini",
		Messages: []model.Message{{
			Role:    "user",
			Content: textContent("hello"),
		}},
		Stream:      boolPtr(false),
		Temperature: float64Ptr(0.7),
	}

	ra := &relayAttempt{
		relayRequest: &relayRequest{internalRequest: base},
		channel:      &dbmodel.Channel{},
	}

	out, err := ra.buildOutboundInternalRequest()
	if err != nil {
		t.Fatalf("buildOutboundInternalRequest() error = %v", err)
	}

	if out.Model != "gpt-4o-mini" {
		t.Fatalf("model = %q, want gpt-4o-mini", out.Model)
	}
	if out.Temperature == nil || *out.Temperature != 0.7 {
		t.Fatalf("temperature not preserved")
	}
}

func TestBuildOutboundInternalRequest_OverrideAndDelete(t *testing.T) {
	override := `{"temperature":0.2,"stream":null}`
	base := &model.InternalLLMRequest{
		Model: "gpt-4o-mini",
		Messages: []model.Message{{
			Role:    "user",
			Content: textContent("hello"),
		}},
		Stream:              boolPtr(true),
		Temperature:         float64Ptr(0.7),
		MaxTokens:           int64Ptr(300),
		RawAPIFormat:        model.APIFormatOpenAIChatCompletion,
		TransformerMetadata: map[string]string{"k": "v"},
		Query:               url.Values{"x": []string{"1"}},
	}

	ra := &relayAttempt{
		relayRequest: &relayRequest{internalRequest: base},
		channel:      &dbmodel.Channel{ParamOverride: &override},
	}

	out, err := ra.buildOutboundInternalRequest()
	if err != nil {
		t.Fatalf("buildOutboundInternalRequest() error = %v", err)
	}

	if out.Temperature == nil || *out.Temperature != 0.2 {
		t.Fatalf("temperature = %v, want 0.2", out.Temperature)
	}
	if out.Stream != nil {
		t.Fatalf("stream should be deleted by null override")
	}
	if out.MaxTokens == nil || *out.MaxTokens != 300 {
		t.Fatalf("max_tokens should remain unchanged")
	}
	if out.Query.Get("x") != "1" {
		t.Fatalf("query metadata should be preserved")
	}
	if out.TransformerMetadata["k"] != "v" {
		t.Fatalf("transformer metadata should be preserved")
	}
}

func TestBuildOutboundInternalRequest_InvalidOverride(t *testing.T) {
	override := `{"temperature":`
	base := &model.InternalLLMRequest{
		Model: "gpt-4o-mini",
		Messages: []model.Message{{
			Role:    "user",
			Content: textContent("hello"),
		}},
	}

	ra := &relayAttempt{
		relayRequest: &relayRequest{internalRequest: base},
		channel:      &dbmodel.Channel{ParamOverride: &override},
	}

	_, err := ra.buildOutboundInternalRequest()
	if err == nil {
		t.Fatal("expected error for invalid json override")
	}
}

func TestBuildOutboundInternalRequest_InvalidAfterOverride(t *testing.T) {
	override := `{"messages":null}`
	base := &model.InternalLLMRequest{
		Model: "gpt-4o-mini",
		Messages: []model.Message{{
			Role:    "user",
			Content: textContent("hello"),
		}},
	}

	ra := &relayAttempt{
		relayRequest: &relayRequest{internalRequest: base},
		channel:      &dbmodel.Channel{ParamOverride: &override},
	}

	_, err := ra.buildOutboundInternalRequest()
	if err == nil {
		t.Fatal("expected validation error when required field removed")
	}
}

func TestCopyHeaders_CustomHeaderDeleteAndOverride(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString("{}"))
	req.Header.Set("X-Remove-Me", "client")
	req.Header.Set("X-Keep", "client")
	req.Header.Set("Authorization", "Bearer client-token")
	c.Request = req

	ra := &relayAttempt{
		relayRequest: &relayRequest{c: c},
		channel: &dbmodel.Channel{CustomHeader: []dbmodel.CustomHeader{
			{HeaderKey: "X-Remove-Me", HeaderValue: ""},
			{HeaderKey: "X-Keep", HeaderValue: "override"},
			{HeaderKey: "X-New", HeaderValue: "added"},
		}},
	}

	outReq, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://example.com/v1/chat/completions", nil)
	if err != nil {
		t.Fatalf("new request error: %v", err)
	}
	outReq.Header.Set("Authorization", "Bearer upstream")

	ra.copyHeaders(outReq)

	if v := outReq.Header.Get("X-Remove-Me"); v != "" {
		t.Fatalf("X-Remove-Me = %q, want deleted", v)
	}
	if v := outReq.Header.Get("X-Keep"); v != "override" {
		t.Fatalf("X-Keep = %q, want override", v)
	}
	if v := outReq.Header.Get("X-New"); v != "added" {
		t.Fatalf("X-New = %q, want added", v)
	}
	if v := outReq.Header.Get("Authorization"); v != "Bearer upstream" {
		t.Fatalf("Authorization should keep outbound default, got %q", v)
	}
}

func TestBuildRequestDiff_RequestOverrideChanges(t *testing.T) {
	before := &model.InternalLLMRequest{
		Model: "gpt-4o-mini",
		Messages: []model.Message{{
			Role:    "user",
			Content: textContent("hello"),
		}},
		Temperature: float64Ptr(0.7),
		Stream:      boolPtr(true),
	}
	after := &model.InternalLLMRequest{
		Model: "gpt-4o-mini",
		Messages: []model.Message{{
			Role:    "user",
			Content: textContent("hello"),
		}},
		Temperature: float64Ptr(0.2),
	}

	diffs := buildRequestDiff(before, after)
	if len(diffs) == 0 {
		t.Fatal("expected non-empty request diff")
	}

	foundTemperature := false
	foundStreamDelete := false
	for _, d := range diffs {
		if d.Path == "/temperature" && d.Operation == dbmodel.DiffOperationReplace {
			foundTemperature = true
		}
		if d.Path == "/stream" && d.Operation == dbmodel.DiffOperationRemove {
			foundStreamDelete = true
		}
	}
	if !foundTemperature {
		t.Fatal("expected temperature replace diff")
	}
	if !foundStreamDelete {
		t.Fatal("expected stream remove diff")
	}
}

func TestCopyHeaders_ReturnsHeaderDiff(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString("{}"))
	req.Header.Set("X-Remove-Me", "client")
	req.Header.Set("X-Keep", "client")
	c.Request = req

	ra := &relayAttempt{
		relayRequest: &relayRequest{c: c},
		channel: &dbmodel.Channel{CustomHeader: []dbmodel.CustomHeader{
			{HeaderKey: "X-Remove-Me", HeaderValue: ""},
			{HeaderKey: "X-Keep", HeaderValue: "override"},
			{HeaderKey: "X-New", HeaderValue: "added"},
		}},
	}

	outReq, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://example.com/v1/chat/completions", nil)
	if err != nil {
		t.Fatalf("new request error: %v", err)
	}
	diffs := ra.copyHeaders(outReq)

	if len(diffs) == 0 {
		t.Fatal("expected non-empty header diff")
	}

	foundAdd := false
	foundRemove := false
	foundReplace := false
	for _, d := range diffs {
		if d.HeaderKey == "X-New" && d.Operation == dbmodel.DiffOperationAdd {
			foundAdd = true
		}
		if d.HeaderKey == "X-Remove-Me" && d.Operation == dbmodel.DiffOperationRemove {
			foundRemove = true
		}
		if d.HeaderKey == "X-Keep" && d.Operation == dbmodel.DiffOperationReplace {
			foundReplace = true
		}
	}
	if !foundAdd {
		t.Fatal("expected add header diff")
	}
	if !foundRemove {
		t.Fatal("expected remove header diff")
	}
	if !foundReplace {
		t.Fatal("expected replace header diff")
	}
}

func TestApplyJSONMergePatch_NestedObjectMergeAndDelete(t *testing.T) {
	base := &model.InternalLLMRequest{
		Model: "gpt-4o-mini",
		Messages: []model.Message{{
			Role:    "user",
			Content: textContent("hello"),
		}},
		Metadata: map[string]string{
			"keep":   "yes",
			"remove": "gone",
		},
	}

	patch := []byte(`{"metadata":{"remove":null,"add":"new"}}`)
	out, err := applyJSONMergePatch(base, patch)
	if err != nil {
		t.Fatalf("applyJSONMergePatch() error = %v", err)
	}
	if out.Metadata["keep"] != "yes" {
		t.Fatalf("metadata.keep = %q, want yes", out.Metadata["keep"])
	}
	if _, ok := out.Metadata["remove"]; ok {
		t.Fatalf("metadata.remove should be deleted")
	}
	if out.Metadata["add"] != "new" {
		t.Fatalf("metadata.add = %q, want new", out.Metadata["add"])
	}
}

func TestApplyJSONMergePatch_RejectNonObjectPatch(t *testing.T) {
	base := &model.InternalLLMRequest{
		Model: "gpt-4o-mini",
		Messages: []model.Message{{
			Role:    "user",
			Content: textContent("hello"),
		}},
	}

	_, err := applyJSONMergePatch(base, []byte(`null`))
	if err == nil {
		t.Fatal("expected error for non-object patch")
	}
}

func textContent(v string) model.MessageContent {
	return model.MessageContent{Content: &v}
}

func boolPtr(v bool) *bool          { return &v }
func float64Ptr(v float64) *float64 { return &v }
func int64Ptr(v int64) *int64       { return &v }
