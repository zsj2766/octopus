package helper

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
)

func TestFetchOpenAIModels_CustomHeaderDeleteAndOverride(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Remove-Me"); got != "" {
			t.Fatalf("X-Remove-Me should be deleted, got %q", got)
		}
		if got := r.Header.Get("X-Override"); got != "new" {
			t.Fatalf("X-Override = %q, want new", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer key-1" {
			t.Fatalf("Authorization = %q, want Bearer key-1", got)
		}

		_ = json.NewEncoder(w).Encode(model.OpenAIModelList{
			Data: []model.OpenAIModel{{ID: "gpt-4o-mini"}},
		})
	}))
	defer server.Close()

	client := server.Client()
	request := model.Channel{
		Name:     "test",
		Type:     outbound.OutboundTypeOpenAIChat,
		BaseUrls: []model.BaseUrl{{URL: server.URL, Delay: 1}},
		Keys: []model.ChannelKey{{
			Enabled:    true,
			ChannelKey: "key-1",
		}},
		CustomHeader: []model.CustomHeader{
			{HeaderKey: "X-Remove-Me", HeaderValue: ""},
			{HeaderKey: "X-Override", HeaderValue: "new"},
		},
	}

	models, err := fetchOpenAIModels(client, context.Background(), request)
	if err != nil {
		t.Fatalf("fetchOpenAIModels() error = %v", err)
	}
	if len(models) != 1 || models[0] != "gpt-4o-mini" {
		t.Fatalf("models = %#v", models)
	}
}
