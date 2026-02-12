package model

import "testing"

func TestSettingValidate_ProxyURLSupportsSocks5(t *testing.T) {
	setting := Setting{Key: SettingKeyProxyURL, Value: "socks5://127.0.0.1:1080"}
	if err := setting.Validate(); err != nil {
		t.Fatalf("expected socks5 proxy url to be valid, got: %v", err)
	}
}

func TestSettingValidate_ProxyURLRejectsUnsupportedScheme(t *testing.T) {
	setting := Setting{Key: SettingKeyProxyURL, Value: "ftp://127.0.0.1:21"}
	if err := setting.Validate(); err == nil {
		t.Fatalf("expected unsupported proxy scheme to be rejected")
	}
}
