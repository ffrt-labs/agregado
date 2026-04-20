package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/felipeafreitas/agregado/internal/api"
	"github.com/felipeafreitas/agregado/internal/broker"
	"github.com/felipeafreitas/agregado/internal/config"
	"github.com/felipeafreitas/agregado/internal/ingestion/rss"
	"github.com/felipeafreitas/agregado/internal/storage"
	"github.com/joho/godotenv"
)

func main() {
	godotenv.Load()
	cfg, err := config.Load()
	if err != nil {
		log.Fatal("Failed to load config", err)
	}
	fmt.Printf("Config loaded: %+v\n", cfg)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	db, err := storage.NewDB(ctx, cfg.Database)

	if err != nil {
		log.Fatal("Failed to load database", err)
	}
	fmt.Printf("Database loaded: %+v\n", db)

	b, err := broker.NewBroker(&cfg.Queue)

	if err != nil {
		log.Fatal("Failed to load broker", err)
	}
	fmt.Printf("Broker loaded: %+v\n", b)
	b.DeclareTopology()

	publisher, err := broker.NewPublisher(b)
	if err != nil {
		log.Fatal("Failed to create publisher", err)
	}
	fmt.Printf("Publisher created: %+v\n", publisher)

	consumer, err := broker.NewConsumer(b)
	if err != nil {
		log.Fatal("Failed to create consumer", err)
	}
	fmt.Printf("Consumer created: %+v\n", consumer)

	sourceRepo := storage.NewSourceRepo(db)
	articleRepo := storage.NewArticleRepo(db)

	parser := rss.NewParser()
	poller := rss.NewPoller(sourceRepo, parser, publisher, cfg.Pooler.Interval)

	handler := storage.NewWorker(articleRepo)

	server := api.NewServer(b, db, cfg.Webhook.Secret)

	go poller.Start(ctx)
	go server.Start(ctx, cfg.Http.Port)
	consumer.Consume("articles.store", handler)

	<-ctx.Done()

	// Shutdown
	shutdownCtx, shutdownCancel :=
  	context.WithTimeout(context.Background(), 5*time.Second)
  	defer shutdownCancel()

	server.Shutdown(shutdownCtx)
	publisher.Close()
	consumer.Close()
	db.Close()
	b.Close()
}
