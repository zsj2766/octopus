package handlers

import (
	"testing"
)

func TestDetectChannelUpdateFieldSet(t *testing.T) {
	tests := []struct {
		name              string
		body              []byte
		wantCustomHeader  bool
		wantParamOverride bool
		wantChannelProxy  bool
		wantMatchRegex    bool
		wantErr           bool
	}{
		{
			name:              "both present",
			body:              []byte(`{"id":1,"custom_header":[],"param_override":null}`),
			wantCustomHeader:  true,
			wantParamOverride: true,
			wantChannelProxy:  false,
			wantMatchRegex:    false,
		},
		{
			name:              "none present",
			body:              []byte(`{"id":1,"name":"x"}`),
			wantCustomHeader:  false,
			wantParamOverride: false,
			wantChannelProxy:  false,
			wantMatchRegex:    false,
		},
		{
			name:              "channel proxy and match regex present",
			body:              []byte(`{"id":1,"channel_proxy":null,"match_regex":""}`),
			wantCustomHeader:  false,
			wantParamOverride: false,
			wantChannelProxy:  true,
			wantMatchRegex:    true,
		},
		{
			name:    "invalid json",
			body:    []byte(`{"id":1`),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotCustomHeader, gotChannelProxy, gotParamOverride, gotMatchRegex, err := detectChannelUpdateFieldSet(tt.body)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if gotCustomHeader != tt.wantCustomHeader {
				t.Fatalf("custom_header set = %v, want %v", gotCustomHeader, tt.wantCustomHeader)
			}
			if gotParamOverride != tt.wantParamOverride {
				t.Fatalf("param_override set = %v, want %v", gotParamOverride, tt.wantParamOverride)
			}
			if gotChannelProxy != tt.wantChannelProxy {
				t.Fatalf("channel_proxy set = %v, want %v", gotChannelProxy, tt.wantChannelProxy)
			}
			if gotMatchRegex != tt.wantMatchRegex {
				t.Fatalf("match_regex set = %v, want %v", gotMatchRegex, tt.wantMatchRegex)
			}
		})
	}
}
