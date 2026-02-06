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

## Session 5: PostgreSQL Storage Layer (Phase 1.6)

**Date:** 2026-02-04

### Topics Covered

#### 1. pgx v5 — Connection Pooling

**pgxpool.New vs pgx.Connect:**
- `pgx.Connect()` — single connection, not concurrency safe
- `pgxpool.New(ctx, connStr)` — creates a connection pool (multiple connections, thread-safe)
- `pgxpool.Connect` existed in v4 but was **removed in v5** — breaking change

**Startup verification:**
- `pgxpool.New` does NOT ping automatically (unlike v4's `Connect`)
- Must call `pool.Ping(ctx)` explicitly to verify DB is reachable at startup
- If Ping fails, close the pool before returning (prevent resource leak)

**Connection string format:**
```
postgresql://user:password@host:port/dbname
```

---

#### 2. The Repository Pattern

**What it is:** A layer that separates domain logic from database access.

**The three layers:**
```
internal/domain/   → WHAT the data looks like (structs)
internal/storage/  → HOW to get data in/out of PostgreSQL (repos)
rest of app        → USES repos without knowing any SQL
```

**Why it matters:**
- The RSS Poller calls `repo.ListActive(ctx)` — no SQL knowledge needed
- If you swap PostgreSQL for another DB later, only the repo changes
- Similar to how `broker/` hides AMQP details from the rest of the app

---

#### 3. Consumer-Side Interfaces (Go Idiom)

**The decision:** Where to define repository interfaces?

**Option A:** Next to the concrete type (one file, one place)
**Option B:** At the point of consumption (idiomatic Go) ← we chose this

**Why Option B:**
- Each consumer declares only what it needs (small interfaces)
- Rob Pike: *"Don't ask for big interfaces when small ones will do"*
- Example: Storage Worker only needs `Create`. It defines its own tiny interface.

**Key insight:** In Go, you write the concrete type first. Interfaces are defined later, when consumers are built. The concrete type doesn't need to know about them.

---

#### 4. pgx Query Patterns

**Three methods, three use cases:**

| Method | Returns | Use when |
|--------|---------|----------|
| `pool.Query(ctx, sql, args...)` | Multiple rows (`pgx.Rows`) | SELECT returning many rows |
| `pool.QueryRow(ctx, sql, args...)` | Single row (`pgx.Row`) | SELECT/INSERT RETURNING one row |
| `pool.Exec(ctx, sql, args...)` | Command result (rows affected) | INSERT/UPDATE/DELETE with no row back |

**Modern row scanning (pgx v5):**
- `pgx.CollectRows(rows, pgx.RowToStructByName[T])` — collects all rows into a slice
- `pgx.CollectOneRow(rows, pgx.RowToStructByName[T])` — collects exactly one row
- These handle `rows.Close()` and `rows.Err()` internally — no manual loop needed

**Manual loop (older pattern):**
```go
defer rows.Close()          // ← AFTER error check, not before
for rows.Next() {
    var s domain.Source
    rows.Scan(&s.ID, &s.Name, ...)  // one row at a time
    result = append(result, s)
}
return result, rows.Err()   // ← check loop errors
```

---

#### 5. Struct Tags: `db:"column_name"` and `db:"-"`

**Purpose:** Tell pgx how to map DB columns ↔ struct fields.

**Same concept as `env` tags from config.go, different library:**
```go
Host        string `env:"DATABASE_HOST"`     // caarlos0/env reads this
EmailSender string `db:"email_sender"`       // pgx reads this
```

**`db:"-"` skips a field entirely:**
```go
Tags []Tag `db:"-"`  // not a DB column — populated via JOIN later
```
Without this, `RowToStructByName` errors if field count ≠ column count.

**Which fields need `db` tags?**
- Any field where the Go name ≠ the column name
- Multi-word fields: `EmailSender` → `email_sender`
- Single words match automatically: `Name` → `name`

---

#### 6. How to Discover Struct Tag Requirements

**The question:** How do you know a function needs struct tags without it being in the docs?

**The answer:** Read the source code. Three steps:
1. Look at the **function name** for clues — "ByName" implies name mapping
2. Search the library source for `Tag.Lookup` or `reflect` — that's where tags are read
3. The tag name before the colon (`db`, `json`, `env`) tells you which library reads it

**pgx's actual matching logic (rows.go):**
- If `db` tag present → use tag value, exact match
- If no tag → use Go field name, **strip underscores from both sides**, compare case-insensitively
- So `EmailSender` actually matches `email_sender` without a tag (both become `emailsender`)
- Tags are still good practice: explicit > implicit

**Key line in pgx source:** `const structTagKey = "db"` — one line defines the magic string.

---

#### 7. SQL Parameterized Queries (Preventing Injection)

**The rule:** Values NEVER go inside the SQL string.

**Wrong (SQL injection):**
```go
fmt.Sprintf("DELETE FROM sources WHERE id = %s", id)
// If id = "'; DROP TABLE sources; --" → catastrophe
```

**Right (parameterized):**
```go
pool.Exec(ctx, "DELETE FROM sources WHERE id = $1", id)
//                                        ^^^^        ^^^^
//                                   placeholder   separate arg
```

**How it works:**
- `$1`, `$2` are pgx placeholders (not fmt verbs!)
- Values are sent separately over the wire — PostgreSQL never sees them as SQL
- `fmt.Sprintf` processes the string BEFORE pgx sees it, so `$1` means nothing to fmt

**PostgreSQL uses `$N` (not `?` like MySQL)** — numbered, so order doesn't matter in the SQL.

---

#### 8. ON CONFLICT — URL-Based Deduplication

**The problem:** RSS poller fetches the same feed every 15 minutes. Re-inserting known articles would either crash (UNIQUE violation) or create duplicates.

**The solution:**
```sql
INSERT INTO articles (external_url, title, ...)
VALUES ($1, $2, ...)
ON CONFLICT (external_url) DO NOTHING
```

- `external_url` is the UNIQUE column (dedup key)
- `DO NOTHING` = if URL already exists, silently skip
- No error, no duplicate, poller can blast freely

**The RETURNING * gotcha:**
- `ON CONFLICT DO NOTHING RETURNING *` returns **zero rows** when conflict fires
- `CollectOneRow` will error on zero rows
- If you don't need the created row back, use `Exec` (no RETURNING) — simplest fix
- Alternative: `DO UPDATE SET col = EXCLUDED.col` forces a row to always be returned

**`EXCLUDED` keyword:** Refers to the values that *would have been* inserted. Useful in `DO UPDATE` to reference the conflicting insert's values.

---

#### 9. Context in Go

**What it is:** A "timeout/cancel ticket" that travels top-down through function calls.

**Three things a context carries:**
1. **Deadline** — "finish by this time or I don't care"
2. **Cancel signal** — "abort, something changed"
3. **Values** — request-scoped data (e.g., request ID)

**The rules:**
- Always the **first parameter**, never stored in a struct
- Name it `ctx`, always
- Pass it down, don't create new ones mid-chain
- Only create at the top: `main`, HTTP handlers

**Why `context.Background()` mid-chain is wrong:**
- It has no deadline, no cancel signal
- Operations using it can hang forever
- The caller's context (with its timeout) gets ignored

**In this project:**
```
NewDB(ctx) → connect(ctx) → pgxpool.New(ctx) → Ping(ctx)
```
If the caller sets a 5-second timeout, Ping auto-cancels after 5s. Zero extra code needed.

---

#### 10. Go Value vs Pointer Patterns

**Returning nil vs zero value:**
```go
func foo() (MyStruct, error)   // value — can't return nil, use MyStruct{}
func foo() (*MyStruct, error)  // pointer — can return nil
```

**Slices are already references:**
```go
// ❌ Never do this
func List() (*[]Article, error)

// ✅ Slices already hold an internal pointer
func List() ([]Article, error)
```

**Resource leak prevention pattern:**
```go
pool, err := pgxpool.New(ctx, connStr)
if err != nil { return err }

if err := pool.Ping(ctx); err != nil {
    pool.Close()   // ← close before returning, or it leaks
    return err
}
```

**`defer` ordering matters:**
```go
rows, err := pool.Query(ctx, sql)
if err != nil { return nil, err }   // ← error check FIRST
defer rows.Close()                  // ← defer AFTER (rows could be nil)
```

**`updated_at` — let the DB decide:**
```sql
-- ✅ DB sets the timestamp (it's happening right now)
UPDATE sources SET name = $1, updated_at = NOW() WHERE id = $2

-- ❌ Don't pass UpdatedAt from the struct (stale value)
```

---

### Files Created/Modified

| File | Purpose |
|------|---------|
| `internal/storage/postgres.go` | pgxpool connection, Ping verification, Close |
| `internal/storage/source_repo.go` | SourceRepo: List, ListActive, Create, Delete, Update |
| `internal/storage/article_repo.go` | ArticleRepo: GetById, List, Create (ON CONFLICT), Delete, MarkRead |
| `internal/domain/source.go` | Added `db` struct tags to all multi-word fields |
| `internal/domain/article.go` | Added `db` struct tags + `db:"-"` on Tags field |

### Design Decisions

1. **pgxpool over single connection** — thread-safe, shared across all repos
2. **Consumer-side interfaces (Option B)** — defined later with consumers, not alongside repos
3. **`RowToStructByName` over positional `RowTo`** — robust against column order changes (e.g. ALTER TABLE adding columns)
4. **`ON CONFLICT DO NOTHING` + `Exec`** — simplest dedup; storage worker doesn't need the row back
5. **`updated_at = NOW()` in SQL** — DB sets timestamps, not the application
6. **Shared `*DB` via constructors** — one pool, many repos. `NewSourceRepo(db)` not `NewSourceRepo(cfg)`

### Key Learnings

**pgx v4 → v5 breaking changes:**
- `pgxpool.Connect` → `pgxpool.New` (no auto-ping)
- Must explicitly Ping after New

**`CollectRows` vs manual loop:**
- `CollectRows` handles Close + Err internally
- No need for `defer rows.Close()` when using it
- Manual loop is still useful to understand the underlying pattern

**Struct tags are metadata, not magic:**
- Libraries read them via `reflect` at runtime
- The tag name (`db`, `json`, `env`) is just a convention — each library defines its own
- You can always find which tag a library uses by searching its source for `Tag.Lookup`

**ON CONFLICT + RETURNING is tricky:**
- `DO NOTHING` means PostgreSQL does nothing — including not returning rows
- This silently breaks `CollectOneRow` (expects exactly one row)
- Know your use case: if you don't need the row back, don't ask for it

---

## Session 6: RSS Poller & Storage Worker (Phases 1.7-1.8)

**Date:** 2026-02-06

### Topics Covered

#### 1. JSON Marshaling & Unmarshaling in Go

**What is marshaling?**
- Marshal = Go struct → bytes (for storage/transmission)
- Unmarshal = bytes → Go struct (for processing)

**JSON encoding/decoding:**
```go
// Marshal: struct → JSON bytes
article := domain.Article{Title: "News", ...}
body, _ := json.Marshal(article)
// body = []byte(`{"title":"News",...}`)

// Unmarshal: JSON bytes → struct
var decoded domain.Article
json.Unmarshal(body, &decoded)
```

**Why JSON for RabbitMQ?**
- Language-agnostic wire format (consumers could be Python, Node.js)
- Stable format independent of Go's internal memory layout
- Human-readable for debugging

**JSON struct tags control behavior:**
```go
type Article struct {
    Title   string    `json:"title"`      // serialize as "title"
    ID      string    `json:"-"`          // skip this field
    Content *string   `json:"content"`    // handles nil automatically
}
```

---

#### 2. The `&` Operator and Taking Addresses

**The problem:** Can't take address of function return values directly.

```go
// ❌ Won't compile
author := &item.Author.Name  // can't take address of method call

// ✅ Store in variable first
authorName := item.Author.Name
author := &authorName
```

**Why needed?** Database fields are `*string` (nullable), but most sources give you `string` values.

**Converting string → *string:**
```go
var summary *string
if item.Description != "" {
    summary = &item.Description  // OK: taking address of struct field
}
// summary is nil if empty, pointer to string if not
```

---

#### 3. Nil vs Empty String Semantics

**The question:** Why use `nil` instead of `""` for optional fields?

**Database semantics:**
- `NULL` (from Go `nil`) = "no data available"
- `""` (empty string) = "explicitly empty content"

**Benefits:**
1. **Clarity** - `nil` means "unknown/missing", not "empty on purpose"
2. **Storage** - PostgreSQL stores `NULL` as a 1-bit flag, `""` takes actual space
3. **API design** - JSON `null` is clearer than `""` for consumers

**Example:**
```go
// RSS feed with no author
item.Author = nil  // Better: clearly no author data

// vs
item.Author = &""  // Ambiguous: is author really "" or just unknown?
```

---

#### 4. Closures in Go

**What is a closure?** A function that captures variables from its surrounding scope.

**The pattern:**
```go
func NewWorker(repo ArticleCreator) func([]byte) error {
    return func(body []byte) error {
        // This inner function "closes over" repo
        // It can access repo even after NewWorker returns
        var article domain.Article
        json.Unmarshal(body, &article)
        return repo.Create(context.Background(), article)
    }
}
```

**Why use closures?**
- Configure dependencies once (at construction time)
- Return a lightweight handler that uses them repeatedly
- Common Go pattern for creating configured handlers

**The flow:**
1. `worker := NewWorker(repo)` - called once at startup, captures `repo`
2. `consumer.Consume("queue", worker)` - passes the handler to consumer
3. Consumer calls `worker(body)` for each message - `repo` is still accessible

---

#### 5. Consumer-Side Interfaces (Revisited)

**Poller defines its own interface:**
```go
// In internal/ingestion/rss/poller.go
type SourceLister interface {
    ListActive(ctx context.Context) ([]domain.Source, error)
    Update(ctx context.Context, source domain.Source) error
}
```

**Worker defines its own interface:**
```go
// In internal/storage/worker.go
type ArticleCreator interface {
    Create(ctx context.Context, article domain.Article) error
}
```

**Key insight:** Each consumer defines the MINIMAL interface it needs. The concrete `*SourceRepo` satisfies `SourceLister` via structural typing - zero glue code!

**Benefits:**
- Poller never imports `storage` package
- Easy to test (pass any struct with those methods)
- Clear contract ("I only need these two methods")

---

#### 6. RSS Feed Parsing with gofeed

**What gofeed does:** Parses multiple feed formats (RSS 2.0, RSS 1.0, Atom) into a unified structure.

**The problem it solves:**
- RSS feeds come in different formats with different XML structures
- Date formats vary (50+ variations across feeds)
- Optional fields in different locations

**Usage:**
```go
parser := gofeed.NewParser()
feed, _ := parser.ParseURL("https://blog.example.com/feed.xml")

for _, item := range feed.Items {
    item.Title            // works for RSS or Atom
    item.Link             // normalized URL
    item.PublishedParsed  // already parsed as *time.Time
    item.Description      // content preview
    item.Content          // full content
}
```

**Key types:**
- `*gofeed.Feed` - feed-level metadata (title, description, items)
- `*gofeed.Item` - individual article
- `*gofeed.Person` - author info (has `.Name` field)

---

#### 7. Ticker Loop Pattern for Periodic Work

**Problem:** Need to poll RSS feeds every 15 minutes, and stop cleanly on shutdown.

**Solution:** `time.Ticker` + `select` + context cancellation

```go
func (p *Poller) Start(ctx context.Context) {
    ticker := time.NewTicker(p.interval)
    defer ticker.Stop()

    p.poll(ctx)  // immediate first poll

    for {
        select {
        case <-ticker.C:
            p.poll(ctx)  // poll on each tick
        case <-ctx.Done():
            return  // graceful shutdown
        }
    }
}
```

**How it works:**
- `ticker.C` sends a value every interval (15m)
- `ctx.Done()` closes when SIGINT/SIGTERM received
- `select` waits for whichever happens first
- `defer ticker.Stop()` cleans up the ticker

---

#### 8. Handling Multiple Authors

**The challenge:** `gofeed.Item` has both:
- `item.Author` - a `*Person` (singular, can be nil)
- `item.Authors` - a `[]*Person` slice (multiple, can be empty)

**The solution:** Check both, concatenate with commas:
```go
var authors []string
if len(item.Authors) > 0 {
    for _, author := range item.Authors {
        if author.Name != "" {
            authors = append(authors, author.Name)
        }
    }
} else if item.Author != nil && item.Author.Name != "" {
    authors = append(authors, item.Author.Name)
}

var author *string
if len(authors) > 0 {
    joined := strings.Join(authors, ", ")
    author = &joined
}
```

**Result:** `author` is `nil` if no authors, otherwise `*string` pointing to "Alice, Bob".

---

#### 9. Filtering Stale Items

**The optimization:** Only publish NEW articles, skip ones we've seen before.

```go
for _, item := range feed.Items {
    // Skip items published before last fetch
    if item.PublishedParsed != nil && source.LastFetchedAt != nil {
        if item.PublishedParsed.Before(*source.LastFetchedAt) {
            continue
        }
    }
    // Process new item...
}
```

**Why needed?**
- Poller runs every 15 minutes
- RSS feeds often include last 10-20 articles
- Without filtering, we'd republish the same 20 articles every 15 minutes

**Nil checks matter:** First poll has `LastFetchedAt = nil`, so we publish everything.

---

#### 10. Error Tracking in Sources

**On parse error:**
```go
if err != nil {
    errMsg := err.Error()
    source.LastError = &errMsg
    source.ErrorCount++
    p.sources.Update(ctx, source)
    continue  // skip to next source
}
```

**On success:**
```go
source.LastError = nil
source.ErrorCount = 0
now := time.Now()
source.LastFetchedAt = &now
p.sources.Update(ctx, source)
```

**Benefits:**
- Track which feeds are broken
- Implement exponential backoff later (if `ErrorCount > 5`, slow down polling)
- Surface errors in web UI ("Source hasn't updated in 3 days")

---

#### 11. context.Background() in Worker

**The situation:** Worker handler signature is `func([]byte) error` - no context parameter.

**Current approach:**
```go
repo.Create(context.Background(), article)
```

**Why it's OK for now:**
- Handler signature is fixed by broker consumer design
- `context.Background()` has no deadline/cancel - could hang forever
- **Phase 5** will add context propagation for distributed tracing

**Better long-term:** Extract request ID from message headers, create context with it.

---

#### 12. Topic Exchange Routing Keys

**What we publish:**
```go
pub.Publish("articles.ingest", "rss", body)
//           ^^^^^^^^^^^^^^^^  ^^^^^
//           exchange          routing key
```

**Why "rss" as routing key?**
- Our binding uses `#` (match anything), so it doesn't matter functionally
- But `"rss"` is **self-documenting** for future filtering
- Later: email ingestion could use key `"email"`
- Could change binding to `rss.*` or `email.*` for selective consumption

**Topic exchange benefit:** Flexible routing without changing publisher code.

---

### Files Created

| File | Purpose |
|------|---------|
| `internal/ingestion/rss/parser.go` | gofeed wrapper with single Parse method |
| `internal/ingestion/rss/poller.go` | Ticker-based RSS polling with error tracking |
| `internal/storage/worker.go` | RabbitMQ consumer handler using closure pattern |

### Files Modified

| File | Changes |
|------|---------|
| `internal/config/config.go` | Added `Poller` struct with `Interval` field |
| `internal/domain/article.go` | Added `json` struct tags for marshaling |
| `docker-compose.yml` | Moved adminer to port 8888 |
| `.env.example` | Added `RSS_POLL_INTERVAL=15m` |

### Design Patterns Used

1. **Wrapper pattern** - `parser.go` wraps `gofeed.Parser` for testability/isolation
2. **Consumer-side interfaces** - Poller/Worker define minimal interfaces, never import concrete repos
3. **Closure pattern** - `NewWorker` returns a function that captures `repo`
4. **Ticker + select pattern** - Periodic work with graceful shutdown
5. **Nil-safe optional fields** - Use `*string` with nil for missing data, not empty strings

---

### Key Learnings

**Marshaling requires struct tags:**
- `json:"field_name"` controls JSON key names
- `json:"-"` excludes fields from JSON
- Only exported (capitalized) fields are marshaled

**Taking addresses of strings:**
- Can't do `&err.Error()` (function return)
- Can do `&item.Description` (struct field)
- For function returns: store in variable first, then take address

**Closures capture by reference:**
- The returned function "remembers" variables from outer scope
- Even after the outer function returns
- Useful for dependency injection without global state

**gofeed handles RSS complexity:**
- 50+ date formats
- Multiple feed types (RSS 2.0, RSS 1.0, Atom)
- Vendor extensions (iTunes, Google News)
- Don't reinvent this wheel!

**Stale item filtering saves bandwidth:**
- RSS feeds repeat old articles
- Filter by `PublishedParsed < LastFetchedAt`
- Reduces RabbitMQ traffic after first poll

**Routing keys for future flexibility:**
- Use meaningful keys ("rss", "email") even if binding is `#`
- Sets you up for selective consumption later
- Self-documenting message origins

---
