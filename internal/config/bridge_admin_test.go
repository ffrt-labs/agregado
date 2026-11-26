package config

import (
	"strings"
	"testing"
)

var bridgeAdminEnv = map[string]string{
	"CLOUDFLARE_ACCOUNT_ID":         "acct",
	"BRIDGE_ADMIN_CF_API_TOKEN":     "cf-token",
	"BRIDGE_D1_DATABASE_ID":         "db-id",
	"BRIDGE_ORIGIN":                 "https://bridge.example.com/",
	"BRIDGE_RECEIVING_DOMAIN":       "news.example.com",
	"MINIFLUX_URL":                  "http://miniflux:8080",
	"BRIDGE_ADMIN_MINIFLUX_API_KEY": "mf-key",
	"BRIDGE_ADMIN_RESEND_API_KEY":   "re-key",
}

func TestLoadBridgeAdmin_ReadsItsResendKey(t *testing.T) {
	for k, v := range bridgeAdminEnv {
		t.Setenv(k, v)
	}
	cfg, err := LoadBridgeAdmin()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Resend.APIKey != "re-key" {
		t.Errorf("Resend.APIKey = %q", cfg.Resend.APIKey)
	}
}

func TestLoadBridgeAdmin_ReadsItsOwnVariables(t *testing.T) {
	for k, v := range bridgeAdminEnv {
		t.Setenv(k, v)
	}
	cfg, err := LoadBridgeAdmin()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Cloudflare.AccountID != "acct" || cfg.Cloudflare.APIToken != "cf-token" || cfg.Bridge.D1DatabaseID != "db-id" ||
		cfg.Bridge.Origin != "https://bridge.example.com" || cfg.Bridge.ReceivingDomain != "news.example.com" ||
		cfg.Miniflux.URL != "http://miniflux:8080" || cfg.Miniflux.APIKey != "mf-key" {
		t.Errorf("cfg = %+v", cfg)
	}
	if cfg.Http.Port != "8080" || cfg.Logging.Format != "json" {
		t.Errorf("defaults: port %q format %q", cfg.Http.Port, cfg.Logging.Format)
	}
}

// The Newsletters app must boot without Agregado's database or enrichment
// secret, and must refuse to boot without any of its own.
func TestLoadBridgeAdmin_NeedsEachOfItsVariablesAndNoneOfAgregados(t *testing.T) {
	for missing := range bridgeAdminEnv {
		t.Run(missing, func(t *testing.T) {
			for k, v := range bridgeAdminEnv {
				if k != missing {
					t.Setenv(k, v)
				}
			}
			t.Setenv(missing, "")
			_, err := LoadBridgeAdmin()
			if err == nil || !strings.Contains(err.Error(), missing) {
				t.Errorf("err = %v, want it to name %s", err, missing)
			}
		})
	}
}
