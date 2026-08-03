// Command test-broker is a manual smoke test for the broker: it declares the
// topology, publishes one message, and waits for the consumer to receive it.
// It logs through the same structured seam as the app — a dev tool that prints
// differently is a dev tool whose output you can't compare to production's.
package main

import (
	"log/slog"
	"os"
	"time"

	"github.com/felipeafreitas/agregado/internal/broker"
	"github.com/felipeafreitas/agregado/internal/config"
	"github.com/felipeafreitas/agregado/internal/logging"
	"github.com/joho/godotenv"
)

const (
	// The topology this smoke test exercises: publish to the ingest exchange,
	// read what lands in the store queue. Named so the log field and the call
	// can't drift apart.
	ingestExchange = "articles.ingest"
	storeQueue     = "articles.store"

	// How long to wait for the round trip before giving up and exiting. Not a
	// timeout — nothing is cancelled — just the observation window.
	processingWait = 3 * time.Second
)

// fatal mirrors cmd/agregado's helper: report structured at ERROR, then exit
// non-zero, preserving the original log.Fatal fail-fast behavior.
func fatal(msg string, err error) {
	slog.Error(msg, "component", "test-broker", "err", err)
	os.Exit(1)
}

func main() {
	// The default logger isn't configured from cfg yet, and both steps below
	// can fail before it could be — install a sane default first so those
	// failures are structured too.
	logging.Setup("info", "json")

	if err := godotenv.Load(); err != nil {
		fatal("failed to load .env file", err)
	}

	cfg, err := config.Load()
	if err != nil {
		fatal("failed to load config", err)
	}
	logging.Setup(cfg.Level, cfg.Format)
	logger := slog.With("component", "test-broker")

	b, err := broker.NewBroker(&cfg.Queue)
	if err != nil {
		fatal("failed to connect to broker", err)
	}
	defer b.Close()

	if err := b.DeclareTopology(); err != nil {
		fatal("failed to declare topology", err)
	}
	logger.Info("topology declared")

	pub, err := broker.NewPublisher(b)
	if err != nil {
		fatal("failed to create publisher", err)
	}
	defer pub.Close()

	consumer, err := broker.NewConsumer(b)
	if err != nil {
		fatal("failed to create consumer", err)
	}
	defer consumer.Close()

	// Message bodies are unbounded, so the payload is a line field — never a
	// label. Same split the rest of the sweep holds to.
	handler := func(body []byte) error {
		logger.Info("message received", "body", string(body))
		return nil
	}

	if err := consumer.Consume(storeQueue, 1, 5, handler); err != nil {
		fatal("failed to start consumer", err)
	}
	logger.Info("consumer started", "queue", storeQueue)

	testMsg := []byte("Hello from test!")
	if err := pub.Publish(ingestExchange, "#", testMsg); err != nil {
		fatal("failed to publish", err)
	}
	logger.Info("message published", "exchange", ingestExchange)

	// Durations go on the wire as strings, matching cmd/agregado/startup.go —
	// a raw time.Duration serializes to bare nanoseconds under the JSON
	// handler, which is the one thing this tool must not do differently.
	logger.Info("waiting for processing", "wait", processingWait.String())
	time.Sleep(processingWait)
	logger.Info("test complete")
}
