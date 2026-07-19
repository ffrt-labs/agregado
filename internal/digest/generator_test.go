package digest

import (
	"strings"
	"testing"
	"time"
)

// TestRender_LocalhostBanner asserts on the generator's public rendered
// output: a digest built with a loopback base URL carries a warning banner,
// one built with a real origin does not. This is the compensating control
// for PUBLIC_BASE_URL silently falling back to its dev default in
// production (see issue #1) — untestable at the config/deploy layer, so the
// banner is what actually gets exercised.
func TestRender_LocalhostBanner(t *testing.T) {
	tests := []struct {
		name       string
		baseURL    string
		wantBanner bool
	}{
		{"dev default", "http://localhost:8080", true},
		{"loopback IP", "http://127.0.0.1:8080", true},
		{"localhost non-default port", "http://localhost:3000", true},
		{"real https origin", "https://agregado.example.com", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g, err := NewDefaultGenerator(nil, tt.baseURL)
			if err != nil {
				t.Fatalf("NewDefaultGenerator: %v", err)
			}

			computed := ComputedDigest{Date: time.Now()}
			email, err := g.Render(computed, nil)
			if err != nil {
				t.Fatalf("Render: %v", err)
			}

			gotBanner := strings.Contains(email.HTML, "PUBLIC_BASE_URL")
			if gotBanner != tt.wantBanner {
				t.Errorf("banner present = %v, want %v (baseURL %q)", gotBanner, tt.wantBanner, tt.baseURL)
			}
		})
	}
}
