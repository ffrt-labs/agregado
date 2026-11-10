// Command seed-langfuse-prompts creates version 1 of each Langfuse-managed
// prompt (agregado/<operation>) at the production label, from the in-code
// default prompts. It is idempotent: a prompt Langfuse already has, in any version,
// is left alone, so it never overwrites an edit made in Langfuse (#201).
//
//	go run ./cmd/seed-langfuse-prompts
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/felipeafreitas/agregado/internal/ai"
	"github.com/felipeafreitas/agregado/internal/config"
	"github.com/felipeafreitas/agregado/internal/langfuse"
	"github.com/felipeafreitas/agregado/internal/logging"
	"github.com/joho/godotenv"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "seed-langfuse-prompts:", err)
		os.Exit(1)
	}
}

func run() error {
	godotenv.Load()
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	logging.Setup(cfg.Level, cfg.Format)

	lf := langfuse.ConfigFrom(cfg.Langfuse)
	if !lf.Enabled() {
		return errors.New("LANGFUSE_BASE_URL, LANGFUSE_PUBLIC_KEY and LANGFUSE_SECRET_KEY are required")
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	return seed(ctx, langfuse.NewPrompts(lf, 0), os.Stdout)
}

// seeder is the part of langfuse.Prompts this command uses.
type seeder interface {
	Seed(ctx context.Context, prompt ai.Prompt) (bool, error)
}

func seed(ctx context.Context, lf seeder, out io.Writer) error {
	for _, op := range ai.TemplateOperations() {
		prompt, _ := ai.DefaultTemplate(op)
		created, err := lf.Seed(ctx, prompt)
		if err != nil {
			return err
		}
		if created {
			fmt.Fprintf(out, "created %s v1 at %s\n", prompt.Name, langfuse.ProductionLabel)
		} else {
			fmt.Fprintf(out, "skipped %s: Langfuse already has it\n", prompt.Name)
		}
	}
	return nil
}
