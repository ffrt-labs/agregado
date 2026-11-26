// Package bridgeadmin is the Newsletters app (cmd/bridge-admin): a small,
// server-rendered, tailnet-only web app that manages the Bridge's Newsletter
// Sources end to end (spec #225, ADR-0012). It reads and writes the Bridge's
// D1 tables and drives Miniflux's API; it never touches Agregado's Postgres.
//
// Each external system sits behind one small interface below. Later slices
// grow them (Resend mail for Confirm, R2 objects for Delete, feed updates for
// Rename/Reconnect) and the tests fake them, per ADR-0002.
package bridgeadmin

import (
	"context"
	"time"

	"github.com/felipeafreitas/agregado/internal/miniflux"
	"github.com/felipeafreitas/agregado/internal/resend"
)

// Newsletter Source statuses, as stored in D1 sources.status. n8n turns mail
// into Articles only for Active (n8n/src/entry.js gateSource).
const (
	StatusPending = "pending"
	StatusActive  = "active"
	StatusPaused  = "paused"
)

// Source is one Newsletter Source row. Alias is sources.id: the lowercase
// local-part of its receiving address, immutable once registered.
type Source struct {
	Alias       string
	DisplayName string
	Status      string
}

// IssueStats summarises a Source's stored issues (D1 entries rows).
type IssueStats struct {
	LastIssueAt *time.Time
	Last30Days  int
}

// SourceStore is the Bridge's D1 (sources and entries tables) as this app
// needs it. D1Store is the real one.
type SourceStore interface {
	Sources(ctx context.Context) ([]Source, error)
	// IssueStats is keyed by alias; Sources with no issues are absent. Last30Days
	// counts issues published at or after since.
	IssueStats(ctx context.Context, since time.Time) (map[string]IssueStats, error)
	SourceExists(ctx context.Context, alias string) (bool, error)
	// InsertPending registers a Source as Pending with the hex SHA-256 of its
	// feed password. It fails if the alias is taken.
	InsertPending(ctx context.Context, source Source, secretHash string) error
	// RemovePending deletes the Source row only while it is still Pending: the
	// rollback for an Add whose Miniflux subscription failed. A no-op when the
	// row is gone or no longer Pending.
	RemovePending(ctx context.Context, alias string) error
	// RenameSource sets a Source's display name, which is its Atom feed title.
	// It fails if there is no such Source.
	RenameSource(ctx context.Context, alias, displayName string) error
	// SetSecretHash replaces a Source's feed password hash (Reconnect). It
	// fails if there is no such Source.
	SetSecretHash(ctx context.Context, alias, secretHash string) error
	// Activate sets a Pending Source to Active and reports whether it did; a
	// Source in any other status, or no Source, is left alone (false).
	Activate(ctx context.Context, alias string) (bool, error)
}

// Miniflux is the reader backend's API as this app needs it. *miniflux.Client
// is the real one.
type Miniflux interface {
	Feeds(ctx context.Context) ([]miniflux.Feed, error)
	Categories(ctx context.Context) ([]miniflux.Category, error)
	CreateCategory(ctx context.Context, title string) (miniflux.Category, error)
	CreateFeed(ctx context.Context, feed miniflux.NewFeed) (int64, error)
	UpdateFeed(ctx context.Context, id int64, update miniflux.FeedUpdate) error
	RefreshFeed(ctx context.Context, id int64) error
}

// ReceivedMail is Resend's receiving API as this app needs it, for Confirm and
// Unclaimed mail. *resend.Client is the real one. Its key can read received
// mail; Resend has no narrower key type (ADR-0012).
type ReceivedMail interface {
	// ReceivedEmails returns the latest resend.ListLimit emails, newest first,
	// only those to the given addresses when any are given. Items carry no body.
	ReceivedEmails(ctx context.Context, recipients ...string) ([]resend.Email, error)
	// ReceivedEmail returns one email with its HTML and text body.
	ReceivedEmail(ctx context.Context, id string) (resend.Email, error)
}
