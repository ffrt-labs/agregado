package broker

import (
	"log/slog"

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
		go worker(queueName, msgs, handler)
	}

	return nil
}

// maxNackBodyLog caps how much of a NACK'd message body is logged, so a large
// newsletter article doesn't flood the log — just enough to identify it.
const maxNackBodyLog = 300

func worker(queueName string, msgs <-chan amqp091.Delivery, handler func([]byte) error) {
	logger := slog.With("component", "broker", "queue", queueName)
	for msg := range msgs {
		err := handler(msg.Body)

		if err != nil {
			msg.Nack(false, false)
			// The real failure surfaces here, at ERROR, the moment it happens —
			// with the handler's error and enough of the body to identify the
			// victim — instead of vanishing silently into the dead-letter queue.
			logger.Error("message nack'd (dead-lettered)",
				"err", err,
				"body", truncateBody(msg.Body, maxNackBodyLog),
			)
		} else {
			msg.Ack(false)
			// Success is routine chatter: demoted below the default level so a
			// line per successful message never buries the ERRORs that matter.
			logger.Debug("message ack'd")
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
