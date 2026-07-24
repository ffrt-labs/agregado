package broker

import "log/slog"

// NewDeadLetterHandler returns a consumer handler for articles.dlq. Every
// message reaching the dead-letter queue is a permanent failure that some
// upstream handler already NACK'd; this handler's whole job is to make that
// failure *visible* — it records the dropped message at ERROR and then acks it
// (returns nil) so the message is removed for good.
//
// It deliberately does not parse, retry, or re-publish the body: returning an
// error here would dead-letter the message again and let a poison message
// loop. The log record is the durable artifact, so the handler is total — it
// drains anything, including malformed or empty bodies.
func NewDeadLetterHandler() func([]byte) error {
	logger := slog.With("component", "deadletter")
	return func(body []byte) error {
		logger.Error("dead-letter message drained",
			"queue", DeadLetterQueue,
			"body", truncateBody(body, maxNackBodyLog),
		)
		return nil
	}
}
