package broker

import (
	"fmt"

	"github.com/rabbitmq/amqp091-go"
)

type Consumer struct {
	ch *amqp091.Channel
}

func NewConsumer(b *Broker) (*Consumer, error) {
	ch, err := b.NewChannel()

	if err != nil {
		return nil, err
	}

	return &Consumer{
		ch: ch,
	}, nil
}

func (c *Consumer) Consume(queueName string, handler func([]byte) error) error {
	const numWorkers = 5

	err := c.ch.Qos(
		1,
		0,
		false,
	)

	if err != nil {
		return err
	}

	msgs, err := c.ch.Consume(
        queueName, // queue
        "",     // consumer
        false,   // auto ack
        false,  // exclusive
        false,  // no local
        false,  // no wait
        nil,    // args
    )

	if err != nil {
		return err
	}

	for w := 1; w <= numWorkers; w++ {
		go worker(msgs, handler)
	}

	return nil
}

func worker(msgs <-chan amqp091.Delivery, handler func([]byte) error) {
	for msg := range msgs {
		err := handler(msg.Body)

		if err != nil {
			msg.Nack(false, false)
			fmt.Printf("Message NACK'd: %v\n", err)
		} else {
			msg.Ack(false)
			fmt.Println("Message ACK'd")
		}
	}
}

func (p *Consumer) Close() error {
	if p.ch == nil {
		return nil
	}

	return p.ch.Close()
}
