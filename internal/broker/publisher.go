package broker

import (
	"context"
	"fmt"
	"time"

	"github.com/rabbitmq/amqp091-go"
)

type Publisher struct {
	ch *amqp091.Channel
}

func NewPublisher(b *Broker) (*Publisher, error) {
	ch, err := b.NewChannel()

	if err != nil {
		return nil, err
	}

	pb := &Publisher{
		ch: ch,
	}

	return pb, nil
}

func (p *Publisher) Publish(exchange string, routingKey string, body []byte) error {
	if p.ch == nil {
		return fmt.Errorf("no channel available")
	}

 	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
  	defer cancel()

	err := p.ch.PublishWithContext(
		ctx,
		exchange,
		routingKey,
		false,
		false,
		amqp091.Publishing{
			ContentType: "application/json",
			Body: body,
			DeliveryMode: amqp091.Persistent,
		},
	)

	return err
}

func (p *Publisher) Close() error {
	if p.ch == nil {
		return nil
	}

	return p.ch.Close()
}
