package config

import "testing"

func TestLoadRequiresAbsolutePreferencesPath(t *testing.T) {
	t.Setenv("DATABASE_USER", "test")
	t.Setenv("DATABASE_PASSWORD", "test")
	t.Setenv("DATABASE_DB", "test")
	t.Setenv("RABBITMQ_USER", "test")
	t.Setenv("RABBITMQ_PASS", "test")
	t.Setenv("ENRICHMENT_SECRET", "test")

	t.Setenv("PREFERENCES_PATH", "preferences.md")
	if _, err := Load(); err == nil {
		t.Fatal("Load() succeeded with a relative PREFERENCES_PATH")
	}

	t.Setenv("PREFERENCES_PATH", "/srv/agregado/preferences/PREFERENCES.md")
	if _, err := Load(); err != nil {
		t.Fatalf("Load() error with an absolute PREFERENCES_PATH: %v", err)
	}
}
