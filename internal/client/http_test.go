package client

import (
	"fmt"
	"net/http"
	"testing"
)

func resetCustomProxyClientCache() {
	clientLock.Lock()
	defer clientLock.Unlock()
	customProxyClients = make(map[string]*http.Client)
	customProxyClientSeq = nil
}

func TestGetHTTPClientCustomProxy_CacheReuse(t *testing.T) {
	resetCustomProxyClientCache()

	first, err := GetHTTPClientCustomProxy("http://127.0.0.1:7890")
	if err != nil {
		t.Fatalf("expected first client without error, got: %v", err)
	}
	second, err := GetHTTPClientCustomProxy("http://127.0.0.1:7890")
	if err != nil {
		t.Fatalf("expected second client without error, got: %v", err)
	}

	if first != second {
		t.Fatalf("expected cached client reuse for same proxy URL")
	}
}

func TestGetHTTPClientCustomProxy_TrimmedURLReuse(t *testing.T) {
	resetCustomProxyClientCache()

	first, err := GetHTTPClientCustomProxy("  http://127.0.0.1:7890  ")
	if err != nil {
		t.Fatalf("expected first client without error, got: %v", err)
	}
	second, err := GetHTTPClientCustomProxy("http://127.0.0.1:7890")
	if err != nil {
		t.Fatalf("expected second client without error, got: %v", err)
	}

	if first != second {
		t.Fatalf("expected trimmed proxy URL to hit same cache entry")
	}
}

func TestGetHTTPClientCustomProxy_Socks5Supported(t *testing.T) {
	resetCustomProxyClientCache()

	client, err := GetHTTPClientCustomProxy("socks5://127.0.0.1:1080")
	if err != nil {
		t.Fatalf("expected socks5 proxy to be supported, got: %v", err)
	}
	if client == nil {
		t.Fatalf("expected non-nil client")
	}
}

func TestGetHTTPClientCustomProxy_BoundedCacheEviction(t *testing.T) {
	resetCustomProxyClientCache()

	for i := range customProxyClientMaxEntries {
		proxyURL := fmt.Sprintf("http://127.0.0.1:%d", 7000+i)
		if _, err := GetHTTPClientCustomProxy(proxyURL); err != nil {
			t.Fatalf("expected proxy %s to be cached, got: %v", proxyURL, err)
		}
	}

	if _, err := GetHTTPClientCustomProxy("http://127.0.0.1:9000"); err != nil {
		t.Fatalf("expected new proxy to trigger eviction without error, got: %v", err)
	}

	clientLock.RLock()
	defer clientLock.RUnlock()
	if len(customProxyClients) != customProxyClientMaxEntries {
		t.Fatalf("expected cache size %d, got %d", customProxyClientMaxEntries, len(customProxyClients))
	}
	if _, exists := customProxyClients["http://127.0.0.1:7000"]; exists {
		t.Fatalf("expected oldest proxy to be evicted")
	}
}
