CREATE TABLE topic_weights (
    topic       VARCHAR(100)    PRIMARY KEY,
    weight      FLOAT           NOT NULL DEFAULT 1.0,
    updated_at  TIMESTAMP       NOT NULL DEFAULT NOW()
);
