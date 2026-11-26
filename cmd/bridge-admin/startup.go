package main

import "github.com/felipeafreitas/agregado/internal/config"

// startupFields is the allowlist of config values the boot line may carry.
// startup_test.go fails on any field not declared safe there, so the
// Cloudflare token and Miniflux key never reach stdout.
func startupFields(cfg *config.BridgeAdmin) []any {
	return []any{
		"component", "main",
		"http_port", cfg.Http.Port,
		"log_level", cfg.Level,
		"log_format", cfg.Format,
		"bridge_origin", cfg.Bridge.Origin,
		"receiving_domain", cfg.Bridge.ReceivingDomain,
		"miniflux_url", cfg.Miniflux.URL,
	}
}
