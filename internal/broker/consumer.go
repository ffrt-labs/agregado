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

// Consume starts numWorkers goroutines draining queueName, acking on success
// and NACKing (no requeue) on failure. prefetch bounds how many unacked
// messages RabbitMQ will hand this consumer at once — with global=false this
// is scoped per consumer, so numWorkers goroutines only run concurrently if
// prefetch is raised to match; at prefetch 1 the broker won't deliver message
// N+1 until N is acked, no matter how many goroutines are waiting.
func (c *Consumer) Consume(queueName string, prefetch, numWorkers int, handler func([]byte) error) error {
	if prefetch <= 0 {
		prefetch = 1
	}
	if numWorkers <= 0 {
		numWorkers = 1
	}

	err := c.ch.Qos(
		prefetch,
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

// maxNackBodyLog caps how much of a NACK'd message body is logged, so a large
// newsletter article doesn't flood the log — just enough to identify it.
const maxNackBodyLog = 300

func worker(msgs <-chan amqp091.Delivery, handler func([]byte) error) {
	for msg := range msgs {
		err := handler(msg.Body)

		if err != nil {
			msg.Nack(false, false)
			fmt.Printf("Message NACK'd: %v (body=%s)\n", err, truncateBody(msg.Body, maxNackBodyLog))
		} else {
			msg.Ack(false)
			fmt.Println("Message ACK'd")
		}
	}
}

func truncateBody(body []byte, max int) string {
	if len(body) <= max {
		return string(body)
	}
	return string(body[:max]) + "...(truncated)"
}

func (p *Consumer) Close() error {
	if p.ch == nil {
		return nil
	}

	return p.ch.Close()
}
