-- Once-per-message failure alerting (agregado#113, plan §2 "Failure
-- handling"). n8n's error workflow does INSERT OR IGNORE keyed on
-- hash(Message-ID) and alerts only when a row was actually inserted, so
-- Resend's ~27h redelivery retries of the same failing message stay quiet.
-- Only n8n touches this table; the Worker's read paths never do.

CREATE TABLE ingest_alerts (
	-- hash(Message-ID), same identity axis as entries.id.
	id TEXT PRIMARY KEY,
	-- Unix seconds of the first failure that alerted.
	alerted_at INTEGER NOT NULL
);
