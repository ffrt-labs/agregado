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

## Session 9: Email Integration (Phase 2.1-2.3)

**Date:** 2026-02-11

### Topics Covered

#### 1. Webhook Secret Validation Pattern

**The requirement:** Only process webhooks from trusted sources (Cloudflare Email Routing).

**The solution:** Validate a shared secret in request headers:
```go
if r.Header.Get("X-Webhook-Secret") != h.secret {
    http.Error(w, "Unauthorized", http.StatusUnauthorized)
    return
}
```

**Security considerations:**
- Secret stored in environment variable (`WEBHOOK_SECRET`)
- Validated BEFORE processing payload (fail fast)
- Use 401 status code for invalid secrets
- Return early to prevent further processing

**Production improvements:**
- Rotate secrets regularly
- Use HMAC signatures instead of plain secrets (more secure)
- Rate limit webhook endpoints to prevent brute force

---

#### 2. HTML to Plain Text Conversion

**The library:** `github.com/jaytaylor/html2text`

**Why needed?** Email newsletters come as HTML, but we want plain text for:
- Storage efficiency (HTML tags add bulk)
- Full-text search (don't want to index `<div>` tags)
- Readable content in digest emails

**Usage:**
```go
text, err := html2text.FromString(payload.Html)
if err != nil || text == "" {
    text = payload.Text // Fallback to plain text
}
```

**Fallback strategy:**
1. Try converting HTML to text
2. If that fails or returns empty, use plain text field
3. Handle emails that only provide one or the other

---

#### 3. Synthetic URLs for Newsletter Articles

**The problem:** Database requires `external_url UNIQUE` for deduplication, but newsletters don't have URLs.

**The solution:** Generate synthetic URLs with UUIDs:
```go
ExternalURL: fmt.Sprintf("newsletter:%s", uuid.New().String())
// Produces: "newsletter:3f4a7c2b-9d1e-4f6a-8b3c-5e7f9a1b2c3d"
```

**Why this works:**
- Guaranteed unique (UUID collision probability ~0)
- Self-documenting prefix (`newsletter:`)
- Satisfies NOT NULL and UNIQUE constraints
- Easy to query (`WHERE external_url LIKE 'newsletter:%'`)

**Alternative approaches:**
- Use sender email + subject + timestamp (risk of duplicates)
- Hash email content (expensive, still risks collisions)
- UUID is simplest and most reliable

---

#### 4. Handler Struct Pattern for Dependency Injection

**The problem:** Handlers need dependencies (config, database, publisher), but function signatures are fixed by the router.

**Wrong approach - global variables:**
```go
var globalRepo *SourceRepo // ❌ Not testable, not thread-safe

func HandleWebhook(w http.ResponseWriter, r *http.Request) {
    globalRepo.Create(...) // Bad!
}
```

**Right approach - handler struct with methods:**
```go
type Handler struct {
    secret    string
    parser    *Parser
    sources   SourceRepository
    publisher *broker.Publisher
}

func (h *Handler) HandleWebhook(w http.ResponseWriter, r *http.Request) {
    h.sources.Create(...) // ✅ Access via h
}
```

**Benefits:**
- Dependencies injected at construction time
- Each handler instance is isolated (testable)
- No global state
- Clear ownership (handler "has" dependencies)

---

#### 5. Package vs Variable Naming Conflicts

**The situation:**
```go
import "github.com/.../broker"

func NewServer(broker *broker.Broker, ...) {
    //          ^^^^^^ variable name
    //                 ^^^^^^ package name
    pub, err := broker.NewPublisher(broker)
    //          ^^^^^^ package       ^^^^^^ variable
}
```

**Why this is valid but confusing:**
- First `broker` = package name (refers to exported symbols)
- Second `broker` = parameter name (refers to the value)
- Go resolves by context

**Better solution:** Rename parameter to avoid confusion:
```go
func NewServer(b *broker.Broker, ...) {
    pub, err := broker.NewPublisher(b)
    //          ^^^^^^ package        ^ variable (clear!)
}
```

**Lesson:** Avoid naming variables the same as package names.

---

#### 6. Interface Signature Matching

**The bug:**
```go
// Interface definition
type SourceRepository interface {
    Create(ctx, source) error  // Returns only error
}

// Actual implementation
func (r *SourceRepo) Create(ctx, source) (*Source, error) {
    // Returns source AND error
}
```

**Problem:** Interface and implementation signatures don't match → compile error!

**The fix:** Make interface match reality:
```go
type SourceRepository interface {
    Create(ctx, source) (*Source, error)  // Now matches
}
```

**Why return the created source?**
- Saves a database round-trip (no need to re-fetch)
- `INSERT ... RETURNING *` already gives us the full row
- Caller can use it immediately

**Updated handler code:**
```go
newSource, err := h.sources.Create(...)
if err != nil { return err }

// Use newSource directly, no re-fetch needed!
article.SourceID = &newSource.ID
```

---

#### 7. Auto-Creating Sources from Email Senders

**The flow:**
```go
// 1. Try to find existing source
source, err := h.sources.FindByEmailSender(ctx, senderEmail)

// 2. If not found, create it
if err != nil {
    source, err = h.sources.Create(ctx, domain.Source{
        Name:        senderEmail,
        Type:        domain.Newsletter,
        EmailSender: &senderEmail,
        IsActive:    true,
    })
}

// 3. Use source ID for article
article.SourceID = &source.ID
```

**Why auto-create?**
- User experience: forward email → automatically tracked
- No manual source setup required
- Sender email uniquely identifies the newsletter

**Database pattern:**
- `FindByEmailSender` queries `WHERE email_sender = $1`
- If no rows → returns error
- Create inserts new source with `email_sender` set
- Future emails from same sender reuse the source

---

#### 8. SQL Placeholder Syntax: $1 vs &1

**Wrong:**
```sql
SELECT * FROM sources WHERE email_sender = &1
                                           ^^^ Wrong!
```

**Right:**
```sql
SELECT * FROM sources WHERE email_sender = $1
                                           ^^^ PostgreSQL placeholder
```

**Different databases use different placeholders:**
| Database | Placeholder | Example |
|----------|-------------|---------|
| PostgreSQL | `$1`, `$2`, `$3` | `WHERE id = $1` |
| MySQL | `?` | `WHERE id = ?` |
| SQLite | `?` or `$1` | Both work |

**pgx uses PostgreSQL syntax** - numbered placeholders starting at `$1`.

---

#### 9. Context Propagation in HTTP Handlers

**The pattern:**
```go
func (h *Handler) HandleWebhook(w http.ResponseWriter, r *http.Request) {
    source, err := h.sources.FindByEmailSender(r.Context(), email)
    //                                         ^^^^^^^^^^^ Request context
}
```

**Why use `r.Context()`?**
- Automatically cancelled if client disconnects
- Carries request-scoped data (trace IDs, etc.)
- Propagates timeouts/deadlines
- Proper cancellation chain: client → handler → database

**Don't use `context.Background()` in handlers:**
- Loses cancellation signal from client
- Can't enforce timeouts
- Operations continue even if client gave up

---

#### 10. Field Visibility and Library Access

**The bug:**
```go
type Webhook struct {
    secret string  // ❌ Lowercase = unexported
}
```

**Problem:** The `env` library uses reflection to set field values. It can only access **exported** (capitalized) fields.

**The fix:**
```go
type Webhook struct {
    Secret string  // ✅ Capitalized = exported
}
```

**Rule:** Any field that needs to be:
- Read/written by libraries (env, json, db)
- Accessed from other packages

MUST be capitalized.

**Exception:** Methods and functions can access unexported fields within the same package.

---

#### 11. JSON Struct Tags for Wire Format

**Why reuse domain entities?**
```go
type Payload struct {
    From    string `json:"from"`
    Subject string `json:"subject"`
    Html    string `json:"html"`
}
```

**Benefits of JSON tags:**
- Controls JSON key names (snake_case, camelCase, etc.)
- Allows Go naming conventions (PascalCase) while using any JSON format
- `json:"-"` excludes fields from serialization

**When to use separate DTOs instead:**
- External API with fixed schema (can't change)
- Wire format very different from domain model
- Need different validation rules

**Our case:** Cloudflare's payload → Go struct is straightforward, so we use structs with tags.

---

#### 12. Variable Shadowing with :=

**The bug:**
```go
source, err := h.sources.FindByEmailSender(ctx, email)
if err != nil {
    err := h.sources.Create(ctx, source)  // ❌ New err variable in scope
    if err != nil { return }
}
```

**Problem:** `:=` inside the if block creates a NEW `err` variable. The outer `err` is shadowed and unused.

**The fix:**
```go
if err != nil {
    err = h.sources.Create(ctx, source)  // ✅ Reuses outer err
    if err != nil { return }
}
```

**When to use `:=` vs `=`:**
- `:=` declares new variables (first use)
- `=` assigns to existing variables (subsequent use)
- Can mix: `x, err := foo()` (x new, err exists) - this works!

---

### Files Created

| File | Purpose |
|------|---------|
| `internal/ingestion/email/webhook.go` | Webhook handler with secret validation |
| `internal/ingestion/email/parser.go` | HTML-to-text parser for email content |
| `internal/config/config.go` | Added Webhook config section |
| `.env.example` | Added WEBHOOK_SECRET |

### Files Modified

| File | Changes |
|------|---------|
| `internal/storage/source_repo.go` | Added FindByEmailSender method |
| `internal/api/server.go` | Wire email handler with dependencies |
| `cmd/agregado/main.go` | Pass webhook secret to server |

### Design Patterns Used

1. **Handler struct pattern** - Dependencies injected at construction, methods access via receiver
2. **Consumer-side interfaces** - Email handler defines SourceRepository interface
3. **Synthetic IDs** - UUIDs for newsletter articles without real URLs
4. **Auto-creation pattern** - Find-or-create for newsletter sources
5. **Fallback strategy** - HTML-to-text with plain text fallback
6. **Context propagation** - Request context flows through all operations

---

### Key Learnings

**Webhook security:**
- Always validate requests before processing
- Use environment variables for secrets
- Return 401 for invalid secrets, 400 for malformed payloads

**HTML email handling:**
- Newsletters usually HTML-only
- Use specialized libraries (html2text) for conversion
- Always have plain text fallback

**Synthetic URLs solve constraint problems:**
- UUID guarantees uniqueness
- Prefix documents the source (`newsletter:`)
- Simpler than hashing content

**Handler struct pattern is idiomatic Go:**
- Constructor injects dependencies
- Methods access via receiver
- No global variables needed
- Easy to test (pass mock dependencies)

**Interface signatures must match exactly:**
- Return types, parameter order, everything
- Go structural typing checks at compile time
- Update interface when implementation changes

**Context propagation is critical:**
- Use `r.Context()` in HTTP handlers
- Pass context to all database/network calls
- Enables cancellation and timeout propagation

**Variable shadowing is subtle:**
- `:=` always declares new variables
- Easy to accidentally shadow with same name
- Go vet warns about some cases
- Pay attention in nested scopes (if/for blocks)

**SQL placeholder syntax varies:**
- PostgreSQL: `$1`, `$2`, `$3`
- MySQL/SQLite: `?`
- Know your database's syntax

**Exported fields required for reflection:**
- Libraries like `env`, `json`, `db` use reflection
- Can only access capitalized (exported) fields
- Compile error won't catch this - runtime failure!

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

## Session 7: Health Endpoints & HTTP Server (Phase 1.9)

**Date:** 2026-02-06

### Topics Covered

#### 1. Chi Router - Lightweight HTTP Routing

**What is Chi?**
- Lightweight, composable HTTP router built on `net/http`
- Provides cleaner routing syntax than stdlib `ServeMux`
- Middleware composition support
- Pattern matching in URLs

**Why use Chi over stdlib?**
```go
// Stdlib - verbose
http.HandleFunc("/health", healthHandler)

// Chi - cleaner, more expressive
r := chi.NewRouter()
r.Get("/health", s.healthHandler)
r.Post("/api/articles", s.createArticle)
```

**Benefits:**
- Built on standard library (minimal abstraction)
- Route variables: `/users/{id}`
- Method-specific handlers (Get, Post, Put, Delete)
- Middleware chains

---

#### 2. Health Check Patterns: Liveness vs Readiness

**Two types of health checks:**

| Check | Purpose | When to use | Return |
|-------|---------|-------------|--------|
| **Liveness** | "Is the app alive?" | `/health` | Always 200 (if reachable) |
| **Readiness** | "Can it handle traffic?" | `/health/db`, `/health/rabbit` | 200 if deps OK, 503 if not |

**Kubernetes/orchestrators use both:**
- **Liveness probe** → if fails, restart the pod
- **Readiness probe** → if fails, remove from load balancer (but don't restart)

**Why separate them?**
- If PostgreSQL is down, the app is still "alive" (don't restart it)
- But it's "not ready" to serve traffic (remove from rotation)
- When PostgreSQL recovers, readiness passes → traffic resumes

**Our implementation:**
```go
GET /health          → {"status":"ok"}  // always 200
GET /health/rabbit   → 200 or 503
GET /health/db       → 200 or 503
```

---

#### 3. Context With Timeout for Health Checks

**The problem:** If PostgreSQL is hung (not just down), `Ping()` could wait forever.

**The solution:** Wrap request context with a timeout:
```go
ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
defer cancel()

err := s.db.Ping(ctx)
```

**How it works:**
- `r.Context()` - the HTTP request's context (cancels if client disconnects)
- `context.WithTimeout(parent, duration)` - creates a **derived context**
- Derived context cancels after 2 seconds OR when parent cancels, whichever comes first
- `defer cancel()` - manually cancel early if Ping succeeds (cleanup)

**Why 2 seconds?**
- Health checks should be fast (orchestrators poll frequently)
- If DB takes > 2s to respond, something is wrong anyway
- Prevents health check itself from hanging

---

#### 4. Pointer Fields in Structs

**The pattern:**
```go
type Server struct {
    broker     *broker.Broker  // pointer
    db         *storage.DB     // pointer
    httpServer *http.Server    // pointer
}
```

**Why pointers, not values?**

| Issue | Value (wrong) | Pointer (right) |
|-------|---------------|-----------------|
| **Copies** | Entire struct copied | Just the address copied |
| **Shared state** | Each Server has its own copy of pool | All Servers share same pool |
| **Nil checking** | Can't represent "no value" | Can be `nil` |

**Resource leak example:**
```go
// ❌ Value type copies the pool
type Server struct {
    db storage.DB  // copies the entire pgxpool
}

// When you call db.Close(), it closes the COPY, not the original!
// The original pool leaks connections.
```

**Correct pattern:**
```go
// ✅ Pointer shares the resource
type Server struct {
    db *storage.DB  // points to the same pool
}

// Now Close() actually closes the shared pool
```

---

#### 5. Method Receivers and Struct Initialization Order

**The problem:** Can't reference methods before the struct exists.

**Wrong approach:**
```go
func NewServer(b *broker.Broker, db *storage.DB) *Server {
    r := chi.NewRouter()
    r.Get("/health", s.healthHandler)  // ❌ s doesn't exist yet!

    s := &Server{...}  // too late
}
```

**Right approach:**
```go
func NewServer(b *broker.Broker, db *storage.DB) *Server {
    r := chi.NewRouter()

    s := &Server{
        broker: b,
        db: db,
        httpServer: &http.Server{Handler: r},
    }

    r.Get("/health", s.healthHandler)  // ✅ now s exists
    return s
}
```

**Key insight:** Create the struct first, THEN register routes that use its methods.

---

#### 6. Channel Cleanup in Health Checks

**The pattern:**
```go
ch, err := s.broker.NewChannel()
if err == nil {
    ch.Close()  // Always clean up!
    // ... return 200
}
// Don't call ch.Close() here - ch is nil on error!
```

**Why close immediately?**
- Each AMQP channel consumes resources on both client and server
- Health checks run every 10-30 seconds
- Without cleanup: leak a channel on every health check → OOM crash

**Why not close in error branch?**
- If `NewChannel()` fails, `ch` is `nil`
- Calling `nil.Close()` panics
- Only close in the success branch

---

#### 7. Goroutines for Non-Blocking Server Start

**The problem:** `ListenAndServe()` blocks forever (until the server stops).

```go
// ❌ This never reaches line 2
func (s *Server) Start(ctx context.Context, port string) error {
    s.httpServer.ListenAndServe()  // blocks forever
    <-ctx.Done()                    // never reached!
}
```

**The solution:** Run server in a background goroutine:
```go
func (s *Server) Start(ctx context.Context, port string) error {
    go func() {
        s.httpServer.ListenAndServe()  // runs in background
    }()

    <-ctx.Done()  // now we can wait for shutdown signal

    // Graceful shutdown...
}
```

**Why goroutines are cheap:**
- OS threads: ~2MB stack, expensive context switching
- Go goroutines: ~2KB stack, cooperatively scheduled
- Can easily run thousands of goroutines

---

#### 8. Graceful Shutdown Pattern

**The full flow:**
```go
func (s *Server) Start(ctx context.Context, port string) error {
    s.httpServer.Addr = fmt.Sprintf(":%s", port)

    // 1. Start server in background
    go func() {
        if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
            // Real errors (not shutdown-related) logged here
        }
    }()

    // 2. Block until shutdown signal
    <-ctx.Done()

    // 3. Graceful shutdown with timeout
    shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    return s.Shutdown(shutdownCtx)
}
```

**Key steps:**
1. **Background start** - server runs without blocking
2. **Wait for signal** - `ctx.Done()` closes when SIGINT/SIGTERM received
3. **New context** - original `ctx` is already canceled, need fresh one for shutdown
4. **5-second timeout** - give active connections time to finish, but not forever

**Why `context.Background()` for shutdown?**
- The original `ctx` is already canceled (that's why we reached this point)
- `Shutdown()` needs a LIVE context with its own timeout
- Otherwise shutdown would fail immediately

---

#### 9. http.ErrServerClosed - Expected Error

**The pattern:**
```go
if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
    // Only log if it's NOT the expected shutdown error
}
```

**Why this check?**
- When `Shutdown()` is called, the server stops gracefully
- `ListenAndServe()` returns `http.ErrServerClosed`
- This is **expected behavior**, not an error to log/handle

**Without the check:**
- Every shutdown logs "server closed" as an error
- Clutters logs with false alarms
- Hard to spot real errors

---

#### 10. HTTP Status Codes for Health Checks

**200 OK:**
- Dependency is healthy and reachable
- Service can handle requests

**503 Service Unavailable:**
- Dependency is down or unreachable
- Service should be removed from load balancer
- But the service itself is still running (don't restart it)

**Why not 500 Internal Server Error?**
- 500 implies a bug in the service code
- 503 implies external dependency issue (not our fault)
- Orchestrators look for 503 to know it's a temporary condition

---

#### 11. JSON Response Pattern

**Setting up JSON responses:**
```go
w.Header().Set("Content-Type", "application/json")
w.WriteHeader(http.StatusOK)
json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
```

**Order matters:**
1. **Set headers first** - must happen before WriteHeader
2. **Write status code** - sends the HTTP response line
3. **Encode body** - writes JSON to response

**Why `json.NewEncoder(w).Encode()` instead of `json.Marshal()`?**
- `Marshal` → bytes → then you write bytes
- `Encoder` writes directly to `http.ResponseWriter` (fewer allocations)
- More idiomatic for HTTP handlers

---

#### 12. The Facade Pattern for DB Access

**What we did:**
```go
type DB struct {
    pool *pgxpool.Pool  // unexported (lowercase)
}

func (db *DB) Ping(ctx context.Context) error {
    return db.pool.Ping(ctx)  // expose through method
}
```

**Why not just export the pool?**
```go
// ❌ Bad
type DB struct {
    Pool *pgxpool.Pool  // exported
}
// Now anyone can call db.Pool.Close() directly!
```

**Benefits of facade:**
- **Encapsulation** - `DB` controls how pool is accessed
- **Future flexibility** - can add logging, metrics, retries
- **Clear interface** - exposes only what's needed

---

### Files Created

| File | Purpose |
|------|---------|
| `internal/api/server.go` | HTTP server with Chi router and health endpoints |

### Files Modified

| File | Changes |
|------|---------|
| `internal/storage/postgres.go` | Added `Ping(ctx)` method to expose pool health check |

### Design Patterns Used

1. **Facade pattern** - DB struct wraps pgxpool, exposes limited interface
2. **Constructor pattern** - `NewServer()` wires dependencies before registering routes
3. **Method receivers** - Health handlers are methods on `*Server`, access dependencies via `s.db`, `s.broker`
4. **Context propagation** - Request context flows through health checks with timeout
5. **Graceful shutdown** - Non-blocking start, wait for signal, clean stop

---

### Key Learnings

**Liveness vs Readiness:**
- Not the same thing!
- Liveness = "is the process alive?" (always return 200)
- Readiness = "can it serve traffic?" (return 503 if deps down)
- Orchestrators use both to make different decisions

**Goroutines are not threads:**
- Extremely lightweight (2KB vs 2MB)
- Scheduled cooperatively by Go runtime
- Can spawn thousands without performance issues

**Context timeout creates a derived context:**
- `WithTimeout(parent, duration)` inherits from parent
- Cancels when either duration expires OR parent cancels
- Always `defer cancel()` even if timeout hasn't fired (cleanup)

**http.ErrServerClosed is expected:**
- Returned by ListenAndServe when Shutdown() is called
- Must explicitly check for it to avoid logging normal shutdowns as errors

**Order of HTTP response matters:**
- Headers → Status Code → Body
- Can't set headers after writing status/body (already sent!)

**Pointer struct fields for shared resources:**
- Value types copy the entire struct
- Pointer types share the same underlying resource
- Critical for connection pools, channels, anything with cleanup

**Method receiver gotcha:**
- Can't reference `s.method` before `s` exists
- Create struct first, then register routes with its methods

**Channel cleanup:**
- Always close AMQP channels after health checks
- Only close in success branch (nil on error)
- Without cleanup: resource leak on every check

---

## Session 8: Main Entry Point & RSS Poller (Phase 1.10)

**Date:** 2026-02-08

### Topics Covered

#### 1. Graceful Shutdown with signal.NotifyContext

**Modern Go pattern (1.16+):**
```go
ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
defer cancel()
```

**What it does:**
- Creates a context that automatically cancels when OS signals arrive
- Converts signals (SIGINT from Ctrl-C, SIGTERM from orchestrators) into context cancellation
- All components use the same cancellation mechanism

**Component integration:**
```go
go poller.Start(ctx)      // Polls until ctx.Done()
go server.Start(ctx, port) // Serves until ctx.Done()
consumer.Consume(queue, h) // Already non-blocking

<-ctx.Done() // Blocks here until signal received
```

**Why reverse order for shutdown?**
1. Stop accepting new work (HTTP server)
2. Stop producing messages (publisher)
3. Stop consuming messages (consumer)
4. Close storage layer (database)
5. Close message broker

This "drains the pipeline" — work in progress completes, no data loss.

---

#### 2. Shutdown Context Must Be Fresh

**The Bug:**
```go
<-ctx.Done()
server.Shutdown(ctx) // ❌ ctx is already cancelled!
```

After `ctx.Done()`, the context is **cancelled**. Using it for shutdown means operations timeout immediately (0 seconds).

**The Fix:**
```go
<-ctx.Done()
shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()
server.Shutdown(shutdownCtx) // ✅ Has 5 seconds to drain
```

Create a **new context** with fresh timeout for graceful shutdown operations.

---

#### 3. Timing Bugs in Polling Systems

**The Critical Bug in poller.go:**

Original code:
```go
// 1. Fetch feed
feed, err := parser.Parse(url)

// 2. Update timestamp to NOW
now := time.Now()
source.LastFetchedAt = &now
sources.Update(ctx, source)

// 3. Filter items published before NOW
for _, item := range feed.Items {
    if item.PublishedParsed.Before(*source.LastFetchedAt) {
        continue // Skip old items
    }
    // Process item...
}
```

**Problem:** Comparing against CURRENT time (NOW), not PREVIOUS fetch time. Since RSS items are always from the past, this skips ALL items on every poll after the first!

**The Fix:**
```go
// 1. Fetch feed (LastFetchedAt still has OLD value from database)
feed, err := parser.Parse(url)

// 2. Filter using OLD timestamp
for _, item := range feed.Items {
    if item.PublishedParsed.Before(*source.LastFetchedAt) {
        continue // Skip items older than PREVIOUS fetch
    }
    // Process item...
}

// 3. Update timestamp AFTER processing
now := time.Now()
source.LastFetchedAt = &now
sources.Update(ctx, source)
```

**Key insight:** In polling systems, always compare against the **previous checkpoint**, not the current time. Sequence matters: read old → compare → update new.

---

#### 4. Error Shadowing with :=

**The Bug:**
```go
publisher, err := broker.NewPublisher(b)
consumer, err := broker.NewConsumer(b) // ❌ err from publisher is lost!
```

`:=` declares a new variable. Using it twice means the second declaration **overwrites** the first error without checking it.

**The Fix:**
```go
publisher, err := broker.NewPublisher(b)
if err != nil {
    log.Fatal("Failed to create publisher", err)
}

consumer, err := broker.NewConsumer(b) // Now safe to reuse err
if err != nil {
    log.Fatal("Failed to create consumer", err)
}
```

**When to use `:=` vs `=`:**
- `:=` declares AND assigns (for new variables)
- `=` assigns to existing variables
- If `err` already exists, use `=` to reuse it
- Common pattern: check error immediately after `:=`

---

#### 5. Component Wiring Order

**Dependency graph determines order:**
```
1. godotenv.Load()          # Load env vars
2. config.Load()            # Parse config
3. signal.NotifyContext()   # Create cancellable context
4. storage.NewDB()          # Needs config.Database
5. broker.NewBroker()       # Needs config.Queue
6. broker.DeclareTopology() # Needs broker
7. broker.NewPublisher()    # Needs broker
8. broker.NewConsumer()     # Needs broker
9. storage repos            # Need db
10. rss.NewPoller()         # Needs repos, publisher, config
11. storage.NewWorker()     # Needs article repo
12. api.NewServer()         # Needs broker, db
```

**Rule:** Never create a component before its dependencies exist.

---

#### 6. Goroutines vs Non-Blocking Operations

**Need `go` keyword:**
```go
go poller.Start(ctx)  // Runs in background, Start() blocks on ticker
go server.Start(ctx, port) // Runs in background, Start() blocks on HTTP
```

These functions **block** until context is cancelled, so they must run in goroutines.

**Don't need `go` keyword:**
```go
consumer.Consume(queue, handler) // Already spawns goroutines internally
```

This function **returns immediately** after spawning internal goroutines. Adding `go` would be redundant.

**How to tell?** Check if the function:
- Loops until context cancellation → needs `go`
- Spawns goroutines and returns → doesn't need `go`

---

#### 7. Consumer-Side Interfaces

**Pattern from Phase 1.6:**
```go
// In internal/ingestion/rss/poller.go
type SourceLister interface {
    ListActive(ctx context.Context) ([]domain.Source, error)
    Update(ctx context.Context, source domain.Source) error
}
```

**Why define interface in consumer package?**
1. **Dependency inversion** — poller doesn't import storage package
2. **Testability** — can mock SourceLister without real database
3. **Structural typing** — `*storage.SourceRepo` satisfies interface automatically (no glue code)

The poller only declares what it needs. The storage repo happens to provide it. Zero coupling.

---

#### 8. JSON Tags for Wire Format

**Added to domain.Article:**
```go
type Article struct {
    ID          string     `db:"id" json:"-"`           // DB-generated, not on wire
    SourceID    *string    `db:"source_id" json:"source_id"`
    ExternalURL string     `db:"external_url" json:"external_url"`
    Title       string     `db:"title" json:"title"`
    CreatedAt   time.Time  `db:"created_at" json:"-"`   // DB-generated
    // ...
}
```

**Why reuse domain types for messaging?**
- Avoids creating separate DTO structs
- Works when wire format matches domain model
- `json:"-"` excludes DB-generated fields (ID, timestamps)

**When NOT to do this?**
- Wire format differs from domain model
- Need different validation rules
- External API with fixed schema

In our case, poller → RabbitMQ → worker all use the same Article structure, so reuse makes sense.

---

#### 9. Port Conflicts

**The Fix:**
Changed `docker-compose.yml`:
```yaml
adminer:
  ports:
    - 8888:8080  # Was 8080:8080
```

**Why?** HTTP server listens on 8080. Adminer on the same port would conflict. Moving adminer to 8888 prevents runtime errors.

**Lesson:** Plan port allocation early:
- 8080: Application HTTP API
- 8888: Adminer (DB admin UI)
- 5432: PostgreSQL
- 5672: RabbitMQ AMQP
- 15672: RabbitMQ Management UI

---

### Design Patterns Used

1. **Signal-driven shutdown** - OS signals → context cancellation → graceful stop
2. **Pipeline drain** - Shutdown in reverse of startup order
3. **Consumer-side interfaces** - Components define their own dependency contracts
4. **Structural typing** - Interfaces satisfied implicitly (no `implements` keyword)
5. **Ticker pattern** - Periodic operations via `time.NewTicker`
6. **Error accumulation** - Log and continue vs fail-fast (appropriate for batch operations)

---

### Key Learnings

**Context cancellation is one-way:**
- Once cancelled, can never be un-cancelled
- Need fresh context for operations after parent cancellation
- Shutdown contexts are always fresh with new timeouts

**Polling checkpoint timing is critical:**
- Compare against OLD checkpoint, not current time
- Update checkpoint AFTER processing, not before
- Order: fetch → filter by old → process → update new

**Error shadowing is subtle:**
- `:=` creates new variables, shadows existing ones
- Always check errors immediately after declaring
- Reuse `err` variable with `=` when intentional

**Component startup order follows dependency graph:**
- Config before anything else
- Connections before clients
- Repositories before services
- Services before HTTP server
- No circular dependencies

**Goroutine discipline:**
- Use `go` for blocking loops/servers
- Don't use `go` for functions that return immediately
- Always provide context for cancellation

**JSON struct tag strategy:**
- Reuse domain types when wire format matches
- Use `json:"-"` for fields that shouldn't serialize
- Snake_case matching DB columns is fine for internal messaging

**Port conflicts happen at runtime:**
- Plan port allocation in advance
- Document in docker-compose.yml and .env.example
- Test with `docker-compose up` early

**Shutdown is as important as startup:**
- Reverse order prevents data loss
- Fresh contexts give operations time to complete
- Log all errors, even during shutdown

---

## Session 10: Digest Ranker Bug Fixes (Phase 3.1)

**Date:** 2026-04-20

### Topics Covered

#### 1. Duration Arithmetic in Go

`time.Duration` represents nanoseconds. `time.Duration(24)` = 24 nanoseconds, not 24 hours.

```go
// ❌ Wrong — 24 nanoseconds ago
time.Now().Add(-time.Duration(lookbackHours))

// ✅ Correct — 24 hours ago
time.Now().Add(-time.Duration(lookbackHours) * time.Hour)
```

**Common duration constants:** `time.Hour`, `time.Minute`, `time.Second`, `time.Millisecond`

---

#### 2. Nil Pointer Guards in sort.Slice

When sorting slices of structs with pointer fields, always guard before dereferencing. The guard must come **before** the dereference, and you need to guard **both** operands.

```go
sort.Slice(articles, func(i, j int) bool {
    if articles[j].PublishedAt == nil {
        return true   // nil dates sort to the end
    }
    if articles[i].PublishedAt == nil {
        return false  // same — push nil to end
    }
    return articles[i].PublishedAt.After(*articles[j].PublishedAt)
})
```

**Why guard `j` first?** You're comparing `i` against `j`. If `j` is nil, the comparison is meaningless — so you decide its position before involving `i`.

---

#### 3. Map Lookups in sort.Slice Closures

Accessing a map by key inside a sort closure works, but is wasteful: a map lookup runs on every comparison pair `(i, j)`.

```go
// ❌ Map lookup on every comparison
sort.Slice(m["uncategorized"], func(i, j int) bool {
    return m["uncategorized"][i].Title > m["uncategorized"][j].Title
})

// ✅ Extract once, use local variable
items := m["uncategorized"]
sort.Slice(items, func(i, j int) bool {
    return items[i].Title > items[j].Title
})
```

This also makes the code easier to read.

---

#### 4. Variable Scope in Closures

Go closures capture variables from the **enclosing scope**. When a variable is redeclared (`:=`) in an inner scope (like a `for` loop), it shadows the outer variable.

```go
for _, tag := range tags {
    articles, ok := articlesMap[tag.ID]  // 'articles' only lives in this loop body
    // ...
}

// ❌ articles and tag are undefined here — loop scope ended
sort.Slice(articles, ...) // references wrong outer 'articles'
```

**Symptom:** No compile error, but the wrong data is sorted or used. Always ask: "which `articles` am I referencing right now?"

---

#### 5. Type Safety in append

`append(slice, elem)` requires the element type to match the slice's element type. Mixing types is a compile error.

```go
var taggedArticles []TaggedArticles

// ❌ Type mismatch — []domain.Article cannot receive a TaggedArticles
taggedArticles = append(articlesMap["uncategorized"], TaggedArticles{...})

// ✅ Correct — append to the right slice
taggedArticles = append(taggedArticles, TaggedArticles{
    Tag:      nil,
    Articles: articlesMap["uncategorized"],
})
```

---

#### 6. Nil Pointer in sort.Slice for Optional Groups

When a slice contains entries with optional pointer fields (`Tag *domain.Tag`), sort comparisons must guard against nil:

```go
sort.Slice(taggedArticles, func(a, b int) bool {
    if taggedArticles[a].Tag == nil {
        return false  // nil (uncategorized) sorts last
    }
    return taggedArticles[a].Tag.Name > taggedArticles[b].Tag.Name
})
```

**Convention:** For UI clarity, categorized content leads; uncategorized falls at the end.

---

### Files Modified

| File | Changes |
|------|---------|
| `internal/digest/ranker.go` | Fixed 4 bugs: duration units, nil guards in sort, type mismatch on append, variable scope in uncategorized block |

### Key Learnings

- `time.Duration(n)` is nanoseconds — multiply by `time.Hour`, `time.Minute`, etc.
- Nil pointer guards must come **before** dereferencing, and guard **both** operands in comparisons
- Extract map values to local variables before sort closures (performance + clarity)
- Closure variable capture follows lexical scope — loop variables don't persist after the loop
- `append` is type-strict: element type must exactly match slice element type

---

## Session 11: Docker Multi-Stage Build & TLS Certificates

**Date:** 2026-06-24

### Topics Covered

#### 1. Multi-Stage Docker Builds and the CA Certificates Gap

**What happened:** The RSS poller failed with `x509: certificate signed by unknown authority` when fetching `https://hnrss.org/frontpage`, even though the same request works fine outside Docker.

**Why multi-stage builds create this problem:**

```dockerfile
FROM golang:1.25 AS builder    # Full Go image — includes CA certificates
...
FROM debian:bookworm-slim      # Stripped runtime image — no CA certs by default
```

The build stage (`golang:1.25`) is a full OS image with `/etc/ssl/certs/` populated. The runtime stage (`debian:bookworm-slim`) is deliberately minimal and **ships without the `ca-certificates` package**. Go's TLS stack reads the system CA bundle at runtime — if it's missing, every HTTPS connection fails with x509 verification errors.

**The fix:** Install `ca-certificates` in the runtime stage:

```dockerfile
FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y ca-certificates && rm -rf /var/lib/apt/lists/*
```

**Why `rm -rf /var/lib/apt/lists/*`?**
- `apt-get update` downloads package index files (~50MB)
- Those indexes are only needed during installation, not at runtime
- Deleting them keeps the final image lean

---

#### 2. Where Go Finds Root CAs

Go's `crypto/tls` package calls into the OS's certificate store at runtime:

| OS | Location |
|----|----------|
| Linux (Debian/Ubuntu) | `/etc/ssl/certs/ca-certificates.crt` |
| Alpine Linux | `/etc/ssl/certs/ca-certificates.crt` (from `ca-certificates` package) |
| macOS | System Keychain (via cgo) |

When you compile with `CGO_ENABLED=0` (as this project does for static binaries), Go uses a pure-Go TLS implementation that still reads the file from disk. No file → no trusted roots → every HTTPS connection fails.

**Alternative for `scratch` or Alpine images:** Copy certs from the builder stage instead of running `apt-get`:

```dockerfile
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
```

---

#### 3. Debugging x509 Errors

The pattern `tls: failed to verify certificate: x509: certificate signed by unknown authority` almost always means one of:

1. **Missing CA bundle** (our case) — runtime image has no trusted root CAs
2. **Self-signed certificate** — server uses a cert not signed by a public CA
3. **Expired certificate** — root CA in the bundle is outdated

In a containerized environment, always suspect (1) first when the request works outside the container but fails inside.

---

### Files Modified

| File | Changes |
|------|---------|
| `Dockerfile` | Added `ca-certificates` installation in runtime stage |

### Key Learnings

- Multi-stage builds inherit **nothing** from the build stage except what you explicitly `COPY`
- `debian:bookworm-slim` omits `ca-certificates` by default — always add it for HTTPS workloads
- Clean up `apt` indexes in the same `RUN` layer to avoid bloating the image
- `CGO_ENABLED=0` (static binary) still needs the OS CA bundle on disk

---

## Session 12: Debugging Production — Cloudflare Tunnel & Container Networking

**Date:** 2026-06-27

**Context:** In production, the app could neither receive newsletter emails (captured by the Cloudflare Worker) nor send digest emails. This session is a *debugging* session — the bug was never in the Go code; every failure was a config/wiring gap at a boundary between components. Classic "never worked in prod" signature.

### The Debugging Method: Evidence at Each Boundary

**Iron law:** No fixes without root-cause investigation first. In a multi-component system, **gather evidence at each component boundary** to find *where* it breaks, instead of guessing.

The inbound email path crosses many boundaries, each a place to probe:
```
Cloudflare Worker  →  public DNS/edge  →  Cloudflare Tunnel  →  cloudflared container
   →  agregado container :8080  →  secret check  →  RabbitMQ publish  →  consumer  →  DB
```

Every step was diagnosed by reading **the actual signal at that boundary** (an HTTP status code, a log line, a `docker ps` status) — not by assumption. The HTTP status alone localizes the break: `401` = secret, `404` = wrong path/route, `502`/`connection refused` = origin unreachable, `200` = that hop is fine.

---

#### 1. Config Propagation Chain (silent empty-string trap)

Env vars travel through **four** links before reaching the app, and any empty link silently passes `""` downstream:
```
GitHub secrets/vars → workflow `env:` block → runner shell → docker compose ${VAR} interpolation → container
```

**Key asymmetry that fingerprints a config gap:** `caarlos0/env` fields marked `required` (DB, RabbitMQ creds) crash the app on boot if missing. Fields with `envDefault` or optional (`WEBHOOK_SECRET`, `SMTP_PASSWORD`, `DIGEST_RECIPIENT_EMAIL`) let the app boot *and misbehave silently*. So "app runs but won't ingest/send" points straight at the optional vars.

**Verify what the container actually sees** (source of truth, not what you *think* you set):
```bash
docker exec <container> env | grep -E 'WEBHOOK_SECRET|SMTP_|...'
```

---

#### 2. Docker Compose Profiles & Service Selection

A newly-added `cloudflared` service didn't start in CI. Two reasons, both about *selection*, not correctness:

- **`docker compose up <name>` starts only the named services + their `depends_on`.** The deploy named only `agregado`, so `cloudflared` (not named, not a dependency) was skipped. Fix: name it — `up -d agregado cloudflared`.
- **Profiles are an opt-in filter.** A service with `profiles: ["prod"]` runs *only* under `--profile prod`. `make dev-deps` runs `docker-compose up` with **no** profile, so prod-only services are intentionally excluded. (That's correct, not a bug — dev doesn't need a tunnel.)

| Command | Profile | Services that come up |
|---|---|---|
| `make dev-deps` → `docker-compose up -d` | none | db, adminer, rabbitmq, mailpit |
| CI deploy → `docker compose --profile prod up -d agregado cloudflared` | prod | agregado **+ cloudflared** |

---

#### 3. Container Networking — `localhost` Means *This Container*

The single most important networking concept of the session. Each container has its **own network namespace**, so `localhost`/`127.0.0.1` refers to the container *making the call*, not its siblings or the host.

This same mistake appeared **three times** at different layers:
1. The Cloudflare Worker (runs on Cloudflare's edge) configured to POST to `localhost`/`192.168.1.142` — neither reachable from off-site.
2. cloudflared's origin Service set to `http://localhost:8080` → `dial tcp [::1]:8080: connection refused` (cloudflared has nothing on its own :8080).
3. The fix in all cases: address the target by the name that's meaningful **from the caller's vantage point**.

**In Docker Compose, sibling containers reach each other by service name** (Compose runs an internal DNS):
```
http://localhost:8080   ❌  (the cloudflared container itself)
http://agregado:8080    ✅  (the agregado service on the shared compose network)
```

> **Recurring principle:** *An address is only meaningful together with the vantage point that resolves it.* A private IP, `localhost`, or a LAN hostname each resolve differently depending on whether you're standing on the host, inside a container, or out on Cloudflare's edge.

---

#### 4. Why Cloudflare Tunnel Was Necessary

The app runs in a **homelab** (self-hosted box) behind home NAT — no public IP, no inbound ports open. But the Cloudflare Worker runs on Cloudflare's **global edge** and must reach the app over the public internet. A private LAN IP (`192.168.x.x`) is not internet-routable, and the Worker's `global_fetch_strictly_public` compatibility flag *explicitly blocks* private/loopback fetches.

**A tunnel inverts the connection direction.** Instead of the internet connecting *in* (port-forwarding, blocked by NAT/CGNAT), a `cloudflared` daemon in the homelab dials *out* to Cloudflare and holds a persistent connection. Cloudflare then routes public requests *down* that pipe.

**A tunnel has two independent legs** — diagnose them separately:
- **Leg 1: edge ↔ cloudflared** — authenticated by `TUNNEL_TOKEN`. Healthy when logs show prechecks pass + `Registered tunnel connection`. A timeout means this leg is down.
- **Leg 2: cloudflared → origin** — the `service:` mapping (`http://agregado:8080`). A **502 / `connection refused`** means leg 1 works but leg 2 doesn't.

---

#### 5. Least-Exposure: Scoping the Tunnel to One Path

A tunnel with a blank path publishes **every** route. The app had ~15 routes and only `/webhook/email` was auth-protected; the rest (`/api/sources` CRUD, `/api/digest/send`, health endpoints leaking `err.Error()`) would be open to the internet.

**Principle of least exposure:** publish the *smallest* surface that meets the requirement. The only external caller is the Worker, hitting one path. So the tunnel's Public Hostname is scoped with a **Path regex** (`^/webhook/email/?$`); everything else falls through to the implicit `http_status:404` catch-all **at Cloudflare's edge** — unreachable, not merely rejected. Anchor the regex (`^...$`) so it can't match `/x/webhook/email/extra`.

---

#### 6. Split-Horizon DNS & NAT Hairpin (the "works on cellular" gotcha)

The tunnel worked from a phone on cellular but appeared broken from inside the home Wi-Fi. Two LAN-side saboteurs:
- **pihole** runs DNS for the LAN and may answer the public hostname with a local/blocked address, so the request never reaches Cloudflare's edge.
- Many home routers can't **hairpin** — loop a request back to your own public IP from inside.

**Rule:** always validate external-facing infra from a genuinely external network. A service can be 100% reachable from the internet yet look broken from your own couch.

---

#### 7. Reading Log Timestamps (the stale-log trap)

A `connection refused` error looked like the fix had failed — but its timestamp was *3 minutes before* the `Updated to new configuration version=N` line. It was the *old* config's error. Tunnel config changes propagate from dashboard → daemon with a delay; each dashboard edit emits a new `version=N`.

**Rule:** only trust log lines timestamped *after* your most recent change. Make a fresh request, then read fresh logs (`docker logs <c> --since 1m`).

---

#### 8. `docker compose` vs raw `docker` CLI

`docker compose <cmd>` is project-scoped — it needs to find `docker-compose.yml`. The plain **`docker` CLI** talks straight to the daemon and sees every container by name, no compose file or working dir needed. For inspecting a running system, raw `docker` is more reliable:
```bash
docker ps -a                               # all containers, status, ports
docker logs <name> --since 1m              # logs without the compose file
docker inspect <name> --format \
  '{{ index .Config.Labels "com.docker.compose.project.working_dir" }}'   # find the compose file
```

### Files Modified

| File | Changes |
|------|---------|
| `docker-compose.yml` | Added `cloudflared` service (`profiles: ["prod"]`, `TUNNEL_TOKEN`) |
| `.github/workflows/deploy.yml` | Added `CLOUDFLARE_TUNNEL_TOKEN` to `env:`; named `cloudflared` in the start step |
| Cloudflare dashboard (tunnel) | Public Hostname → Service `http://agregado:8080`, Path `^/webhook/email/?$` |
| Cloudflare Worker secret | `WEBHOOK_URL` → `https://agregado.felipefreitas.dev/webhook/email` |

### Key Learnings

- **Debug at boundaries, not in the code.** "Never worked in prod" almost always means a wiring/config gap between components that were each tested in isolation. Gather one fact per boundary.
- **`localhost` is per-container.** Sibling containers talk via service names on the shared Compose network, never `localhost`.
- **An address needs a vantage point.** Worker edge, private LAN IP, container loopback, pihole DNS — the same string resolves to different things from different places.
- **Config has a propagation chain.** Empty GitHub secret → empty workflow env → empty `${VAR}` → silent default. `required` vs optional env fields explains *which* symptom you get.
- **Compose `up <name>` selects services explicitly**, and `profiles` filter them; a perfect compose file does nothing if the command doesn't select the service.
- **A tunnel = outbound connection inverting NAT**, with two independently-diagnosable legs (token/edge vs origin service).
- **Least exposure:** scope public ingress to the one path that must be public; let the edge 404 the rest.
- **Test external infra externally** (cellular), and **trust only logs newer than your last change.**

---

## Session: Digest 24h window — `ingested_at` vs `published_at`

### Topic Covered

#### Filtering on the right timestamp

The daily digest was surfacing months-old posts. Root cause: `FindUnreadSince`
(`internal/storage/article_repo.go`) filtered `WHERE ingested_at > $1` — *when
Agregado fetched the item* — instead of `published_at` — *when the source
published it*. An old post re-fetched today therefore looked "new" to the
digest. The lookback plumbing (`LookbackHours` → `since`) was already correct;
only the column was wrong.

### Key Learnings

- **Two timestamps, two meanings.** `published_at` (source's clock, **nullable**
  — feeds may omit it) vs `ingested_at` (our clock, always set). Pick the one
  that matches the *question*: "published in the last 24h" → `published_at`.
- **`COALESCE(published_at, ingested_at)` for nullable fallback.** Returns the
  publish date when present, else fetch time — so undated feeds/newsletters
  aren't silently dropped from the digest.
- **Filter and sort must use the same expression.** Filtering on `COALESCE(...)`
  but ordering by bare `published_at DESC` would float NULL-dated rows to the top
  (Postgres's default `DESC` NULL ordering). Match both.
- **Sargability trade-off.** Wrapping a column in `COALESCE(...)` makes the
  `published_at` btree index unusable for this predicate (non-sargable → filtered
  scan). Acceptable at current volume; noted for later.

---

## Session: Digest ranking — sort by score, not date

### Topic Covered

#### Ordering the digest by the score we already have

The digest grouped articles by topic but then sorted each group by
`PublishedAt`, discarding the relevance order. Fixed in
`internal/digest/ranker.go`: within each group, sort by `RelevanceScore` DESC
then `PublishedAt` DESC; order the groups so the topic with the highest-scored
article leads (Uncategorized pinned last).

### Key Learnings

- **The AI ranking signal already exists.** `relevance_score` is assigned by the
  scorer at ingest. Re-ranking by that stored int is free and instant; calling
  an LLM at render time would just re-derive the same number, slowly.
- **`sort.Slice` needs a strict weak ordering.** A comparator must satisfy: if
  `less(a,b)` then `!less(b,a)`, and `less(a,a)` is false. Return `false` for
  equal keys (including both-nil) or sorts can corrupt.
- **Nil-pointer keys need an explicit rank.** `RelevanceScore`/`PublishedAt` are
  `*int`/`*time.Time`. Handle nil on both sides before dereferencing and decide
  where nil sorts (here: last, matching SQL `NULLS LAST`). A small
  `scoreOrZero(*int) int` helper makes the primary-key compare total and
  removes the nil branching.
- **Primary/secondary sort keys.** Compare the primary key; only fall through to
  the tiebreaker when it's equal. Extracting one comparator
  (`lessByScoreThenDate`) removed two divergent copies of the logic.

---
