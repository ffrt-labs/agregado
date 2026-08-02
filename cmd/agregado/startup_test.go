package main

import (
	"bytes"
	"log/slog"
	"reflect"
	"strings"
	"testing"

	"github.com/felipeafreitas/agregado/internal/config"
)

// safeFields are the config values startup is allowed to put on the wire. Every
// other string in Config — DB password, RabbitMQ password, Cloudflare API
// token, SMTP password, webhook secret — is either a credential today or one
// bad rename away from being one, so the test treats "not on this list" as
// "must not appear in the log".
var safeFields = map[string]bool{
	"Http.Port":      true,
	"AI.Model":       true,
	"Logging.Level":  true,
	"Logging.Format": true,
	"Database.Host":  true,
	"Database.Port":  true,
	"Queue.Host":     true,
	"Queue.Port":     true,
}

// bootLines is every log line boot emits from config, keyed by message. Adding
// one that isn't here means it escapes the leak check below.
var bootLines = map[string]func(*config.Config) []any{
	"agregado starting":  startupFields,
	"database connected": databaseFields,
	"broker connected":   brokerFields,
}

// TestStartupFieldsLeakNoSecrets is the grep-the-stdout acceptance criterion
// turned into a test: fill every string in Config with a field-unique sentinel,
// emit the startup line through a JSON handler like the one logging.Setup
// installs, and fail if any sentinel that isn't explicitly allowed shows up in
// the bytes. A future field added to Config is covered automatically — either
// seedStrings can seed it, or it fails the test until the seeder learns how.
func TestStartupFieldsLeakNoSecrets(t *testing.T) {
	cfg := &config.Config{}
	sentinels := seedStrings(t, reflect.ValueOf(cfg).Elem(), "")

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	for msg, fields := range bootLines {
		logger.Info(msg, fields(cfg)...)
	}
	out := buf.String()

	for path, sentinel := range sentinels {
		present := strings.Contains(out, sentinel)
		if present && !safeFields[path] {
			t.Errorf("boot log leaked config field %s (value %q)\nlog output: %s", path, sentinel, out)
		}
		if !present && safeFields[path] {
			t.Errorf("boot log is missing expected non-sensitive field %s\nlog output: %s", path, out)
		}
	}
}

// TestBootFieldsAreWellFormed pins the key/value shape slog requires: an
// odd-length args slice degrades to a "!BADKEY" record, which would silently
// mangle the lines an operator sees at boot. go vet catches this at a literal
// call site; these fields are built one indirection away from theirs.
func TestBootFieldsAreWellFormed(t *testing.T) {
	for msg, build := range bootLines {
		fields := build(&config.Config{})

		if len(fields)%2 != 0 {
			t.Errorf("%q: fields must be key/value pairs, got odd length %d", msg, len(fields))
			continue
		}
		for i := 0; i < len(fields); i += 2 {
			if _, ok := fields[i].(string); !ok {
				t.Errorf("%q: field %d is a key and must be a string, got %T", msg, i, fields[i])
			}
		}
	}
}

// seedStrings walks a struct (recursing into embedded/nested structs) and gives
// every string field a value unique to its path, returning path -> value.
//
// A kind it can't seed fails the test rather than being skipped: a silently
// unseeded field is a credential this test would swear is safe. Numbers and
// bools (ports, timeouts, char limits) are the one exemption — they can't carry
// a secret string, and Config is full of them.
func seedStrings(t *testing.T, v reflect.Value, prefix string) map[string]string {
	t.Helper()

	seeded := map[string]string{}
	typ := v.Type()
	for i := 0; i < typ.NumField(); i++ {
		field, value := typ.Field(i), v.Field(i)
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
		case reflect.Bool,
			reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
			reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
			reflect.Float32, reflect.Float64:
			// Numeric/bool config can't smuggle a credential string out.
		default:
			t.Fatalf("config field %s has kind %s, which seedStrings can't seed — "+
				"teach it that kind, or this test stops covering that field", path, value.Kind())
		}
	}
	return seeded
}
