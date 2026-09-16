// Command migrate-ownership moves the old Agregado Postgres data to whichever
// tool finally owns it (issue #83). It defaults to a dry run: an apply has to
// be asked for, because one of its two destinations is a third-party service
// with no undo.
//
//	go run ./cmd/migrate-ownership                 # dry run, the default
//	go run ./cmd/migrate-ownership -apply          # really write
//	go run ./cmd/migrate-ownership -samples 20     # compare more records
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/felipeafreitas/agregado/internal/config"
	"github.com/felipeafreitas/agregado/internal/karakeep"
	"github.com/felipeafreitas/agregado/internal/logging"
	"github.com/felipeafreitas/agregado/internal/ownership"
	"github.com/felipeafreitas/agregado/internal/storage"
	"github.com/joho/godotenv"
)

func main() {
	apply := flag.Bool("apply", false, "write to Karakeep and the Article Index (default: dry run)")
	samples := flag.Int("samples", 10, "how many representative records to compare before and after")
	flag.Parse()

	if err := run(*apply, *samples); err != nil {
		fmt.Fprintln(os.Stderr, "migrate-ownership:", err)
		os.Exit(1)
	}
}

func run(apply bool, samples int) error {
	godotenv.Load()
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	logging.Setup(cfg.Level, cfg.Format)

	// Checked here rather than in config.Load: the server has no business
	// talking to Karakeep, so requiring these globally would break its boot.
	if cfg.Karakeep.Address == "" || cfg.Karakeep.APIKey == "" {
		return fmt.Errorf("KARAKEEP_ADDRESS and KARAKEEP_API_KEY are required")
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	db, err := storage.NewDB(ctx, cfg.Database)
	if err != nil {
		return fmt.Errorf("connect to the old database: %w", err)
	}
	defer db.Close()

	bookmarker := karakeep.New(cfg.Karakeep.Address, cfg.Karakeep.APIKey, cfg.Karakeep.Timeout)
	// Checked up front, in a dry run too: a bad address or a revoked key makes
	// every existence check answer "not bookmarked", and the migration would
	// duplicate the entire Pile instead of skipping it.
	if err := bookmarker.Ping(ctx); err != nil {
		return fmt.Errorf("karakeep is not reachable with these credentials: %w", err)
	}

	write := ownership.DryRun
	if apply {
		write = ownership.Apply
	}

	report, err := ownership.NewRunner(
		storage.NewLegacyRepo(db),
		bookmarker,
		storage.NewOwnershipImportRepo(db),
		samples,
	).Run(ctx, write)
	if err != nil {
		// A run that died partway still produced counts worth reading, and is
		// safe to repeat once the cause is fixed.
		fmt.Print(report.Render())
		return err
	}

	fmt.Print(report.Render())

	// A failed item is not a failed process — the report is the deliverable —
	// but the exit code has to say so, or a scripted run looks clean.
	var failed int
	for _, class := range ownership.Classes {
		failed += report.Counts[class].Failed
	}
	if failed > 0 {
		return fmt.Errorf("%d items did not move; see FAILURES above", failed)
	}
	return nil
}
