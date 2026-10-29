package backup

import (
	"context"
	"testing"
	"time"

	"github.com/felipeafreitas/agregado/internal/config"
)

// An unschedulable backup must fail Start instead of leaving an empty cron
// blocked on ctx.Done(), which would look healthy while never sending.
func TestStartRejectsUnschedulableSpec(t *testing.T) {
	for name, spec := range map[string]string{
		"empty":     "",
		"malformed": "not a cron spec",
	} {
		t.Run(name, func(t *testing.T) {
			s := NewScheduler(nil, nil, config.Backup{Schedule: spec})

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			done := make(chan error, 1)
			go func() { done <- s.Start(ctx) }()

			select {
			case err := <-done:
				if err == nil {
					t.Fatalf("Start(%q) returned nil, want an error", spec)
				}
			case <-time.After(time.Second):
				t.Fatalf("Start(%q) blocked instead of failing", spec)
			}
		})
	}
}
