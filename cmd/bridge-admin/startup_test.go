package main

import (
	"bytes"
	"log/slog"
	"reflect"
	"strings"
	"testing"

	"github.com/felipeafreitas/agregado/internal/config"
)

// safeFields are the config values boot may log. Everything else in
// BridgeAdmin (the Cloudflare token, the Miniflux key, and whatever later
// slices add, such as the Resend key) must stay out of stdout.
var safeFields = map[string]bool{
	"Http.Port":              true,
	"Logging.Level":          true,
	"Logging.Format":         true,
	"Bridge.Origin":          true,
	"Bridge.ReceivingDomain": true,
	"Miniflux.URL":           true,
}

// TestStartupFieldsLeakNoSecrets mirrors cmd/agregado's: seed every string in
// the config with a sentinel and fail if a sentinel not on safeFields reaches
// the boot line.
func TestStartupFieldsLeakNoSecrets(t *testing.T) {
	cfg := &config.BridgeAdmin{}
	sentinels := seedStrings(t, reflect.ValueOf(cfg).Elem(), "")

	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("bridge-admin starting", startupFields(cfg)...)
	out := buf.String()

	for path, sentinel := range sentinels {
		present := strings.Contains(out, sentinel)
		if present && !safeFields[path] {
			t.Errorf("boot log leaked config field %s\nlog output: %s", path, out)
		}
		if !present && safeFields[path] {
			t.Errorf("boot log is missing non-sensitive field %s\nlog output: %s", path, out)
		}
	}
	if fields := startupFields(cfg); len(fields)%2 != 0 {
		t.Errorf("startup fields must be key/value pairs, got %d", len(fields))
	}
}

func seedStrings(t *testing.T, v reflect.Value, prefix string) map[string]string {
	t.Helper()
	seeded := map[string]string{}
	for i := 0; i < v.NumField(); i++ {
		field, value := v.Type().Field(i), v.Field(i)
		if !value.CanSet() {
			continue
		}
		path := prefix + field.Name
		switch value.Kind() {
		case reflect.Struct:
			for nested, sentinel := range seedStrings(t, value, path+".") {
				seeded[nested] = sentinel
			}
		case reflect.String:
			sentinel := "sentinel-" + strings.ReplaceAll(path, ".", "-")
			value.SetString(sentinel)
			seeded[path] = sentinel
		case reflect.Bool, reflect.Int, reflect.Int64, reflect.Float64:
		default:
			t.Fatalf("config field %s has kind %s, which seedStrings can't seed", path, value.Kind())
		}
	}
	return seeded
}
