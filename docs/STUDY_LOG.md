# Agregado - Study Log

A log of key learnings and insights gained during the development of this project.

---

## Session 1: Database Layer (Phase 1.2)

**Date:** 2026-01-27

### Topics Covered

#### 1. Database Migrations

**What are migrations?**
- Versioned SQL scripts that evolve your database schema over time
- Come in pairs: `up.sql` (applies changes) and `down.sql` (reverts changes)
- Naming convention: `{version}_{description}.up.sql`

**Why use migrations?**
- Version control for database schema
- Reproducible environments (dev, staging, prod)
- Safe rollbacks if something goes wrong

**golang-migrate:**
- Tracks applied migrations in a `schema_migrations` table
- `version` column shows which migration number was applied
- `dirty` flag indicates if a migration failed halfway

---

#### 2. Primary Keys

**Purpose:**
1. Uniquely identifies each row (no duplicates, no NULLs)
2. Automatically creates an index for fast lookups

**UUIDs vs Auto-increment:**
- UUIDs (`gen_random_uuid()`) are better for distributed systems
- No coordination needed between servers
- Harder to guess (security benefit)

---

#### 3. Database Indexes

**The problem:** Without indexes, finding a row requires scanning every row (O(n))

**The solution:** Indexes are data structures (usually B-trees) that map values to row locations (O(log n))

| Rows | Without Index | With Index |
|------|---------------|------------|
| 1,000 | 1,000 checks | ~10 checks |
| 100,000 | 100,000 checks | ~17 checks |
| 10,000,000 | 10,000,000 checks | ~24 checks |

**Trade-offs:**
- Indexes take disk space
- Slow down INSERT/UPDATE (must update index too)
- Only index columns you frequently search or join on

**Types of indexes used:**
- **Regular index:** Fast lookups on a single column
- **Composite index:** Multiple columns, order matters (`source_id, published_at DESC`)
- **Partial index:** Only indexes some rows (`WHERE NOT is_read`)
- **GIN index:** For full-text search with `to_tsvector()`

---

#### 4. Foreign Keys and Referential Integrity

**ON DELETE CASCADE:**
- When parent row is deleted, child rows are automatically deleted
- Example: Delete a source → all its articles are deleted too

**Other options:**
- `ON DELETE SET NULL` - Sets foreign key to NULL
- `ON DELETE RESTRICT` - Prevents deletion if children exist

---

#### 5. Junction Tables (Many-to-Many Relationships)

**The problem:** One digest has many articles, one article can be in many digests

**The solution:** A junction table (`digest_articles`) with:
- Foreign keys to both tables
- Composite primary key `(digest_id, article_id)`

```
digest_logs ←──── digest_articles ────► articles
```

---

#### 6. CHECK Constraints

**Purpose:** Enforce valid values at the database level

```sql
type VARCHAR(50) NOT NULL CHECK (type IN ('rss', 'newsletter', 'manual'))
```

- Database rejects invalid values
- Safer than relying on application code alone

---

#### 7. VARCHAR vs TEXT in PostgreSQL

| Type | Max Length | Use Case |
|------|-----------|----------|
| `VARCHAR(n)` | n chars (enforced) | When you want length validation |
| `TEXT` | Unlimited | When length is unbounded |

**Note:** In PostgreSQL, performance is identical. VARCHAR is purely for validation.

---

#### 8. Full-Text Search in PostgreSQL

**Components:**
- `to_tsvector('english', text)` - Converts text to searchable tokens
- `GIN` index - Maps words to rows containing them
- `COALESCE(content, '')` - Handles NULL values in concatenation

```sql
CREATE INDEX idx_articles_search ON articles
    USING GIN (to_tsvector('english', title || ' ' || COALESCE(content, '')));
```

---

#### 9. Makefile and Environment Variables

**Problem:** Makefile doesn't read `.env` files automatically

**Solution:** Add at the top of Makefile:
```makefile
include .env
export
```

---

### Schema Decisions Made

1. **Added `manual` source type** - Allows users to save articles by URL (not just RSS/newsletters)
2. **URL-based deduplication** - `external_url UNIQUE` prevents duplicate articles
3. **Partial index for unread** - Optimizes the most common query
4. **Audit logging** - `digest_logs` tracks what was sent and when

---

### Files Created

| File | Purpose |
|------|---------|
| `migrations/000001_initial_schema.up.sql` | Creates 5 tables + 3 indexes |
| `migrations/000001_initial_schema.down.sql` | Drops everything in reverse order |

### Tables Created

1. `sources` - RSS feeds, newsletters, manual sources
2. `articles` - Main content with URL deduplication
3. `digest_logs` - Audit log of sent digests
4. `digest_articles` - Junction table (digest ↔ article)
5. `preferences` - Key-value settings store

---

## Session 2: Configuration & Domain Entities (Phase 1.3 & 1.4)

**Date:** 2026-01-28

### Topics Covered

#### 1. Environment Variable Configuration with `caarlos0/env`

**Why this library?**
- Simple: struct tags + one function call
- Type-safe: parses directly to typed fields
- Self-documenting: struct tags show what env vars are needed
- Minimal dependencies: zero external deps
- Built-in validation: `required` tag fails fast

**Struct tags syntax:**
```go
type Config struct {
    Host string `env:"DATABASE_HOST" envDefault:"localhost"`
    User string `env:"DATABASE_USER,required"`
}
```

| Tag | Purpose |
|-----|---------|
| `env:"VAR_NAME"` | Maps field to environment variable |
| `envDefault:"value"` | Fallback if not set |
| `required` | Fails if variable is missing |

**Loading pattern:**
```go
func Load() (*Config, error) {
    cfg := &Config{}
    if err := env.Parse(cfg); err != nil {
        return nil, err
    }
    return cfg, nil
}
```

---

#### 2. Go Struct Tags

**What are they?**
- Metadata strings attached to struct fields (in backticks)
- Libraries read them at runtime using the `reflect` package
- Format: `key:"value"` pairs, space-separated

**Common uses:**
| Library | Tag | Purpose |
|---------|-----|---------|
| `encoding/json` | `json:"field_name"` | JSON serialization |
| `caarlos0/env` | `env:"VAR_NAME"` | Env var mapping |
| `gorm` | `gorm:"primaryKey"` | Database ORM |

---

#### 3. Pointers in Go

**The `&` operator:**
- `&Config{}` creates a struct AND returns a pointer to it
- Pointers hold memory addresses, not values

**Why return pointers from functions?**
1. **Efficiency**: Avoids copying the entire struct
2. **Nil semantics**: Can return `nil` to indicate "no value"

**Visual:**
```
cfg := &Config{}

   cfg (pointer)          (actual struct in memory)
   ┌─────────┐           ┌─────────────────┐
   │ 0x1234 ─┼──────────►│ Database: ...   │
   └─────────┘           │ Queue: ...      │
                         └─────────────────┘
```

---

#### 4. Shell Environment Variables

**Problem:** `source .env` sets variables but doesn't export them to child processes

**Solution:** Use `set -a` to auto-export:
```bash
set -a && source .env && set +a && go run cmd/agregado/main.go
```

| Command | What it does |
|---------|--------------|
| `set -a` | Auto-export all variables defined from now on |
| `source .env` | Load the .env file |
| `set +a` | Turn off auto-export |

**Why needed:** `go run` creates a child process that can only see exported variables.

---

#### 5. Domain Entities and Nullable Fields

**Rule for mapping database columns to Go types:**
- Column has `NOT NULL` or `DEFAULT` → use value type (`string`, `int`, `time.Time`)
- Column is nullable → use pointer type (`*string`, `*int`, `*time.Time`)

**Why pointers for nullable?**
- `*string` can be `nil` (represents SQL NULL)
- `string` would become `""` — loses distinction between NULL and empty

**Custom types for type safety:**
```go
type Type string

const (
    Rss        Type = "rss"
    Newsletter Type = "newsletter"
    Manual     Type = "manual"
)
```
This prevents invalid values at compile time.

---

#### 6. Go Naming Conventions

- Acronyms should be all caps: `URL`, `ID`, `HTTP`
- Package names should match directory: `internal/domain/` → `package domain`
- Only `cmd/*/main.go` should have `package main`

---

#### 7. Unix Philosophy: Silence is Golden

`go build ./...` with no output = success!

In Unix tools, no news is good news. Errors are printed, success is silent.

Check exit code with `echo $?` (0 = success).

---

### Files Created

| File | Purpose |
|------|---------|
| `internal/config/config.go` | Configuration loading from env vars |
| `internal/domain/source.go` | Source entity with type constants |
| `internal/domain/article.go` | Article entity with nullable fields |
| `cmd/agregado/main.go` | Entry point (for testing config) |

---

## Session 3: Article Tagging (Phase 1.4b)

**Date:** 2026-01-29

### Topics Covered

#### 1. Many-to-Many Relationships with Junction Tables

**The problem:** An article can have multiple tags, and a tag can be on multiple articles.

**The solution:** Create a junction table (`article_tags`) with:
- Two foreign key columns referencing each table
- Composite primary key from both columns

```sql
CREATE TABLE article_tags (
    article_id UUID REFERENCES articles(id) ON DELETE CASCADE,
    tag_id UUID REFERENCES tags(id) ON DELETE CASCADE,
    PRIMARY KEY (article_id, tag_id)
);
```

**Why composite primary key?**
- Ensures uniqueness: same article-tag pair can't exist twice
- No need for a separate `id` column
- Both columns together form the unique identifier

---

#### 2. REFERENCES (Foreign Key Constraints)

**What it does:** Creates a link between tables that the database enforces.

```sql
tag_id UUID REFERENCES tags(id) ON DELETE CASCADE
```

**This means:**
1. **Validation** - PostgreSQL rejects any `tag_id` that doesn't exist in `tags.id`
2. **Referential integrity** - Can't have orphaned references

**ON DELETE options:**

| Option | Behavior | Use Case |
|--------|----------|----------|
| `CASCADE` | Delete related rows too | Junction tables, child records |
| `SET NULL` | Set FK column to NULL | Optional relationships |
| `RESTRICT` | Block the delete | Prevent accidental data loss |

**We used:**
- `article_tags` → `CASCADE` (if tag deleted, remove associations)
- `sources.default_tag_id` → `SET NULL` (if tag deleted, source keeps existing but loses default)

---

#### 3. Migration Order Matters

**Creating tables (up.sql):**
1. Create referenced table first (`tags`)
2. Create referencing table second (`article_tags`)
3. Alter existing tables last (`sources`)

**Dropping tables (down.sql) - reverse order:**
1. Remove foreign key columns first (`ALTER TABLE sources DROP COLUMN`)
2. Drop referencing tables (`article_tags`)
3. Drop referenced tables last (`tags`)

**Why?** Can't drop a table that others reference.

---

#### 4. Seeding Data in Migrations

**INSERT in migrations:**
```sql
INSERT INTO tags (name, slug, color) VALUES
    ('Tech', 'tech', '#3B82F6'),
    ('Business', 'business', '#10B981'),
    ...
```

**When to seed in migrations:**
- Predefined/static data (categories, roles, statuses)
- Data that the application expects to exist

**When NOT to seed:**
- User-generated data
- Test data (use separate fixtures)

---

#### 5. Go Field Visibility (Exported vs Unexported)

**The rule:** Capitalized = exported (public), lowercase = unexported (private)

```go
// ❌ Wrong - fields can't be accessed outside package
type Tag struct {
    name string   // unexported
}

// ✅ Correct - fields are accessible
type Tag struct {
    Name string   // exported
}
```

**Why it matters:**
- JSON marshaling only works with exported fields
- Other packages (like repositories) can't access unexported fields
- Go's visibility is at the package level, not class level

---

#### 6. Domain Entity Design Decisions

**Nullable FK in Source:**
```go
DefaultTagID *string  // pointer = nullable
```
- A source might not have a default tag
- Using `*string` allows `nil` to represent "no default"

**Tags in Article:**
```go
Tags []Tag  // slice of full Tag structs
```
- Could have used `[]string` for just IDs
- Full structs allow displaying tag name/color without extra queries
- Trade-off: slightly more memory, but simpler code

---

#### 7. SQL Syntax Gotchas

**Trailing commas:**
```sql
-- ❌ Syntax error
created_at TIMESTAMP DEFAULT NOW(),
);

-- ✅ Correct
created_at TIMESTAMP DEFAULT NOW()
);
```

**Missing commas:**
```sql
-- ❌ Syntax error
ON DELETE CASCADE
PRIMARY KEY (...)

-- ✅ Correct
ON DELETE CASCADE,
PRIMARY KEY (...)
```

---

### Files Created/Modified

| File | Purpose |
|------|---------|
| `migrations/000002_add_tags.up.sql` | Creates tags, article_tags, adds default_tag_id |
| `migrations/000002_add_tags.down.sql` | Reverses all changes in correct order |
| `internal/domain/tag.go` | Tag entity with Name, Slug, Color |
| `internal/domain/source.go` | Added `DefaultTagID *string` field |
| `internal/domain/article.go` | Added `Tags []Tag` field |

### Predefined Tags Created

| Name | Slug | Color |
|------|------|-------|
| Tech | tech | #3B82F6 |
| Business | business | #10B981 |
| Personal | personal | #8B5CF6 |
| Politics | politics | #EF4444 |
| Economy | economy | #F59E0B |
| Science | science | #06B6D4 |
| Health | health | #EC4899 |
| Entertainment | entertainment | #F97316 |

---

## Session 4: RabbitMQ Integration (Phase 1.5)

**Date:** 2026-02-02

### Topics Covered

#### 1. AMQP Connection Management

**Connection vs Channel:**
- **Connection** - TCP connection to RabbitMQ broker (heavyweight)
- **Channel** - Logical connection within a Connection (lightweight, can create many)
- Channels are NOT thread-safe - each goroutine needs its own

**Best practice:**
- Broker owns the Connection
- Provide `NewChannel()` method for consumers/publishers
- Each consumer/publisher creates its own channel

---

#### 2. Exponential Backoff Pattern

**Problem:** If RabbitMQ is starting up, immediate connection failure crashes the app.

**Solution:** Retry with increasing delays:
```
Attempt 1: wait 1s
Attempt 2: wait 2s
Attempt 3: wait 4s
Attempt 4: wait 8s
Attempt 5: wait 16s (capped at 30s)
```

**Key concepts:**
- `delay = delay * 2` - doubles each time (exponential)
- Cap at max delay to prevent excessive waits
- Only sleep if retrying (not after final failure)

**Implementation pattern:**
```go
for attempt := 1; attempt <= maxAttempts; attempt++ {
    if success { return nil }
    if attempt < maxAttempts {
        time.Sleep(delay)
        delay = min(delay * 2, maxDelay)
    }
}
return error
```

---

#### 3. RabbitMQ Topology

**Exchanges:**
- **Purpose:** Routes messages to queues based on rules
- **Topic exchange:** Routes by pattern matching (`#` = match all)
- **Fanout exchange:** Broadcasts to all bound queues (DLX pattern)

**Queues:**
- **Purpose:** Stores messages until consumed
- **Durable:** Survives RabbitMQ restarts (persisted to disk)

**Bindings:**
- **Purpose:** Connects exchange → queue with routing rules
- `QueueBind(queue, routingKey, exchange)`

**Our topology:**
```
Normal flow:
  Publisher → articles.ingest (exchange) → articles.store (queue) → Consumer

Failed messages (DLX pattern):
  Consumer NACK → articles.dlx (exchange) → articles.dlq (queue)
```

---

#### 4. Dead Letter Exchange (DLX) Pattern

**Problem:** What happens when message processing fails? Infinite retries? Data loss?

**Solution:** Send failed messages to a separate queue for inspection.

**How it works:**
1. Queue declares DLX in arguments: `x-dead-letter-exchange: articles.dlx`
2. Consumer NACKs a message with `requeue=false`
3. RabbitMQ republishes message to the DLX
4. DLX routes to DLQ (dead-letter queue)

**Why useful:**
- Failed messages don't block processing
- Can inspect/debug failures later
- Prevents infinite retry loops

---

#### 5. Message Persistence

**DeliveryMode: Persistent**
```go
amqp091.Publishing{
    DeliveryMode: amqp091.Persistent,
    Body:         body,
}
```

**What it does:**
- RabbitMQ writes message to disk
- Survives broker restarts
- Slightly slower than memory-only

**Combined with durable queues:** Full persistence guarantee.

---

#### 6. Worker Pool Pattern

**Problem:** Processing messages one-by-one is slow.

**Solution:** Spawn multiple goroutines (workers) to process in parallel.

```go
for w := 1; w <= numWorkers; w++ {
    go worker(msgs, handler)
}
```

**Each worker:**
1. Receives messages from shared channel
2. Calls handler function
3. ACKs or NACKs based on result

**Key: Goroutines are cheap** - can easily run 5-10 workers per consumer.

---

#### 7. QoS (Quality of Service) - Prefetch

**Problem:** Without limits, RabbitMQ might send ALL messages to one worker.

**Solution:** Set prefetch limit:
```go
ch.Qos(
    1,     // prefetchCount - max unacked messages per worker
    0,     // prefetchSize - 0 = no limit
    false, // global - false = per-consumer
)
```

**With 5 workers and prefetch=1:**
- Max 5 messages "in flight" at once
- Fair distribution across workers
- If worker dies, only 1 message needs redelivery

---

#### 8. ACK/NACK Semantics

**ACK (Acknowledge):**
```go
msg.Ack(false)  // multiple=false (just this message)
```
- Tells RabbitMQ: "I processed this successfully"
- Message removed from queue permanently

**NACK (Negative Acknowledge):**
```go
msg.Nack(false, false)  // multiple=false, requeue=false
```
- First `false` - don't NACK multiple messages
- Second `false` - don't requeue (send to DLX instead)

**If requeue=true:** Message goes back to original queue (can cause infinite loops!)

---

#### 9. Context with Timeout

**Problem:** Publishing might hang if RabbitMQ is overloaded.

**Solution:** Use context with timeout:
```go
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

pub.PublishWithContext(ctx, ...)
```

**What happens:**
- If publish takes > 5s, returns error
- `defer cancel()` ensures resources are cleaned up
- Prevents indefinite blocking

---

#### 10. Go Concurrency - Goroutines and Channels

**Goroutines:**
```go
go worker(msgs, handler)  // Spawns lightweight thread
```
- Managed by Go runtime
- Much cheaper than OS threads
- Can spawn thousands easily

**Channels (Go channels, not AMQP channels!):**
```go
msgs <-chan amqp091.Delivery  // Receive-only channel
```
- Thread-safe message passing between goroutines
- `for msg := range msgs` - loops until channel closes

---

#### 11. Defer for Cleanup

**Pattern:**
```go
ch, err := b.NewChannel()
if err != nil {
    return err
}
defer ch.Close()  // Guaranteed to run when function exits
```

**Benefits:**
- Resource cleanup happens even if errors occur
- Keeps cleanup code close to allocation
- Common Go idiom

---

#### 12. Environment Variables in Go

**Problem:** `.env` files aren't automatically loaded by Go.

**Solutions:**
1. **Use godotenv library:**
   ```go
   godotenv.Load()  // Loads .env into os.Environ
   ```

2. **Export in shell:**
   ```bash
   export $(cat .env | xargs)
   ```

**Why needed:** `env.Parse()` only reads from OS environment, not files.

---

### Files Created

| File | Purpose |
|------|---------|
| `internal/broker/rabbitmq.go` | Connection manager, topology setup |
| `internal/broker/publisher.go` | Message publishing with persistence |
| `internal/broker/consumer.go` | Worker pool consumer with ACK/NACK |
| `cmd/test-broker/main.go` | Integration test for broker components |

### Architecture Decisions

1. **Broker owns connection, provides channels** - Better concurrency model than shared channel
2. **Exponential backoff (1s → 30s, 5 attempts)** - Resilient startup during RabbitMQ initialization
3. **5 workers per consumer** - Balanced parallelism for message processing
4. **Prefetch = 1** - Fair distribution, limits redelivery on worker failure
5. **DLX pattern** - Failed messages go to `articles.dlq` for debugging, not infinite retries
6. **Persistent delivery** - Messages survive RabbitMQ restarts

---

### Key Learnings

**RabbitMQ != Go channels:**
- AMQP channels are network abstractions (not thread-safe)
- Go channels are in-memory concurrency primitives (thread-safe)
- Confusing naming, completely different concepts!

**Exponential vs Quadratic backoff:**
- Exponential: `delay = delay * 2` (2, 4, 8, 16...)
- Quadratic: `delay = delay * delay` (2, 4, 16, 256...) - TOO aggressive!

**DLX flow has 3 hops:**
1. Consumer NACKs from `articles.store`
2. RabbitMQ republishes to `articles.dlx` exchange
3. Exchange routes to `articles.dlq` queue
(The exchange is the intermediary, messages don't go directly to DLQ)

**Message flow visualization:**
```
Normal:    Publisher → articles.ingest → articles.store → Consumer ACK ✓
Failed:    Consumer NACK → articles.dlx → articles.dlq (for inspection)
```

---
