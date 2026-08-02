package main

import "github.com/felipeafreitas/agregado/internal/config"

// The functions below are the allowlist for everything boot puts on the wire:
// short sets of non-sensitive values, named one by one. They exist as their own
// functions so the choice of what reaches stdout is testable — dumping the whole
// Config (or db, or broker) would put the database password and the Cloudflare
// API token verbatim into every log store that tails stdout.
//
// Naming a field here is the only way a config value reaches the boot log, and
// startup_test.go fails on any field that isn't explicitly declared safe.

// startupFields is the "agregado starting" line: what this process was
// configured to be, before it touches anything external.
func startupFields(cfg *config.Config) []any {
	return []any{
		"component", "main",
		"http_port", cfg.Http.Port,
		"ai_model", cfg.AI.Model,
		"log_level", cfg.Level,
		"log_format", cfg.Format,
		"rss_poll_interval", cfg.Pooler.Interval.String(),
	}
}

// databaseFields marks a successful database connection. Host and port only —
// the storage.DB itself holds the credentials it dialed with — but that's
// enough to tell "pointed at the wrong instance" from "never got there".
func databaseFields(cfg *config.Config) []any {
	return []any{
		"component", "main",
		"db_host", cfg.Database.Host,
		"db_port", cfg.Database.Port,
	}
}

// brokerFields marks a successful broker connection, on the same terms.
func brokerFields(cfg *config.Config) []any {
	return []any{
		"component", "main",
		"broker_host", cfg.Queue.Host,
		"broker_port", cfg.Queue.Port,
	}
}
