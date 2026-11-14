package config

import "testing"

func setRequired(t *testing.T) {
	t.Helper()
	for _, k := range []string{"DATABASE_USER", "DATABASE_PASSWORD", "DATABASE_DB", "RABBITMQ_USER", "RABBITMQ_PASS", "ENRICHMENT_SECRET"} {
		t.Setenv(k, "x")
	}
}

// The production cutover (#84) turns the legacy RSS poller off so Miniflux is
// the only thing fetching feeds; rollback turns it back on. docker-compose
// passes an unset variable through as "", so empty must mean the default.
func TestLoad_RSSPollerEnabled(t *testing.T) {
	cases := []struct {
		name  string
		value string
		set   bool
		want  bool
	}{
		{name: "unset defaults to on", set: false, want: true},
		{name: "empty (compose passthrough) defaults to on", value: "", set: true, want: true},
		{name: "false turns it off", value: "false", set: true, want: false},
		{name: "true keeps it on", value: "true", set: true, want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setRequired(t)
			if tc.set {
				t.Setenv("RSS_POLLER_ENABLED", tc.value)
			}
			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if cfg.Pooler.Enabled != tc.want {
				t.Fatalf("Pooler.Enabled = %v, want %v", cfg.Pooler.Enabled, tc.want)
			}
		})
	}
}
