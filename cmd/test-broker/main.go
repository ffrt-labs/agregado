package main

import (
	"fmt"
	"log"
	"time"

	"github.com/felipeafreitas/agregado/internal/broker"
	"github.com/felipeafreitas/agregado/internal/config"
	"github.com/joho/godotenv"
)

func main() {
	if err := godotenv.Load(); err != nil {
        log.Fatal("Failed to load .env file:", err)
    }

	cfg, err := config.Load()
	if err != nil {
		log.Fatal("Failed to load config:", err)
	}

	b, err := broker.NewBroker(&cfg.Queue)
	if err != nil {
		log.Fatal("Failed to connect to broker:", err)
	}
	defer b.Close()

	if err := b.DeclareTopology(); err != nil {
		log.Fatal("Failed to declare topology:", err)
	}
	fmt.Println("✓ Topology declared")

    pub, err := broker.NewPublisher(b)
    if err != nil {
        log.Fatal("Failed to create publisher:", err)
    }
    defer pub.Close()

    consumer, err := broker.NewConsumer(b)
    if err != nil {
        log.Fatal("Failed to create consumer:", err)
    }
    defer consumer.Close()

    handler := func(body []byte) error {
   		fmt.Printf("✓ Received message: %s\n", string(body))
        return nil
    }

    if err := consumer.Consume("articles.store", handler); err != nil {
    	log.Fatal("Failed to start consumer:", err)
    }
    fmt.Println("✓ Consumer started")

    testMsg := []byte("Hello from test!")
    if err := pub.Publish("articles.ingest", "#", testMsg); err != nil {
        log.Fatal("Failed to publish:", err)
    }
    fmt.Println("✓ Message published")

    // 8. Wait to see message processed
    fmt.Println("Waiting 3 seconds for processing...")
    time.Sleep(3 * time.Second)
    fmt.Println("✓ Test complete!")
}
