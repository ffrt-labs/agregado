package broker

import (
	"fmt"
	"time"

	"github.com/felipeafreitas/agregado/internal/config"
	"github.com/rabbitmq/amqp091-go"
)

type Broker struct {
	conn *amqp091.Connection
	config *config.Queue
}

func NewBroker(cfg *config.Queue) (*Broker, error) {
	b := &Broker{config: cfg}
	if err := b.connect(); err != nil {
		return nil, err
	}
	return b, nil
}

func (b *Broker) connect() error {
	connStr := fmt.Sprintf(
		"amqp://%s:%s@%s:%s/",
		b.config.User,
		b.config.Password,
		b.config.Host,
		b.config.Port,
	)

	maxAttempts := 5
	delay := 1 * time.Second
	maxDelay := 30 * time.Second

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		conn, err := amqp091.Dial(connStr)

		if err == nil {
			b.conn = conn
			return nil

		}

		if attempt < maxAttempts {
			time.Sleep(delay)
			delay = delay * 2

			delay = min(delay, maxDelay)
		}
	}

	return fmt.Errorf("failed to connect after %d attempts", maxAttempts)
}

func (b *Broker) NewChannel() (*amqp091.Channel, error) {
	if b.conn == nil {
		return nil, fmt.Errorf("no connection established")
	}

	return b.conn.Channel()
}

func (b *Broker) Close() error {
	if b.conn == nil {
		return nil
	}

	return b.conn.Close()
}

func (b *Broker) DeclareTopology() error {
	ch, err := b.NewChannel()

	if err != nil {
		return err
	}

	defer ch.Close()

	err  = ch.ExchangeDeclare(
		"articles.ingest",
		"topic",
		true,
		false,
		false,
		false,
		nil,
	)

	if err != nil {
		return err
	}

	err  = ch.ExchangeDeclare(
		"articles.dlx",
		"fanout",
		true,
		false,
		false,
		false,
		nil,
	)

	if err != nil {
		return err
	}

	_, err = ch.QueueDeclare(
		"articles.store",
		true,
		false,
		false,
		false,
		amqp091.Table{
			"x-dead-letter-exchange": "articles.dlx",
		},
	)

	if err != nil {
		return err
	}

	_, err = ch.QueueDeclare(
		"articles.dlq",
		true,
		false,
		false,
		false,
		nil,
	)

	if err != nil {
		return err
	}

	err = ch.QueueBind(
		"articles.store",
		"#",
		"articles.ingest",
		false,
		nil,
	)

	if err != nil {
		return err
	}

	err = ch.QueueBind(
		"articles.dlq",
		"",
		"articles.dlx",
		false,
		nil,
	)

	return err
}
