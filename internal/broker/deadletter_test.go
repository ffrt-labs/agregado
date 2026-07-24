package broker

import "testing"

// TestDeadLetterHandlerDrains pins the one contract that matters for the DLQ
// consumer: it never fails a message, whatever the body looks like. Returning
// an error would NACK the message back into the dead-letter machinery and
// recreate the exact "poison message loops forever" black hole this consumer
// exists to close. The body is a durable log record, not something to parse or
// re-drive — so malformed JSON and an empty body must drain just like a valid
// enrichment trigger does.
func TestDeadLetterHandlerDrains(t *testing.T) {
	handler := NewDeadLetterHandler()

	cases := []struct {
		name string
		body []byte
	}{
		{"valid enrichment trigger", []byte(`{"article_id":"abc-123"}`)},
		{"malformed json", []byte(`{"article_id":`)},
		{"empty body", []byte(``)},
		{"nil body", nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := handler(tc.body); err != nil {
				t.Errorf("handler returned err = %v, want nil (message must drain, never re-fail)", err)
			}
		})
	}
}
