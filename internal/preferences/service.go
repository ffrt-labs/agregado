package preferences

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/felipeafreitas/agregado/internal/preferenceexport"
)

// Source records who wrote a stored version: the reader through the Doc, or
// the nightly regeneration.
const (
	SourceManual      = "manual"
	SourceRegenerated = "regenerated"
)

type Store interface {
	// Current returns the latest stored profile, or ErrNoProfile.
	Current(ctx context.Context) (string, error)
	// Save stores text as the newest version unless it equals the current
	// one, and reports whether it did.
	Save(ctx context.Context, text, source string) (bool, error)
}

type Signals interface {
	Signals(ctx context.Context) ([]preferenceexport.Signal, error)
}

// Rewriter is the one model call that regenerates the whole profile from the
// current one plus rendered Reader evidence.
type Rewriter interface {
	RegeneratePreferences(ctx context.Context, current, evidence string) (string, error)
}

// maxSignals bounds the evidence in one regeneration prompt. Votes are kept
// first because they are strong evidence; Opens fill what is left, newest
// first.
const maxSignals = 300

// maxSummaryChars bounds each Signal's Summary in the evidence.
const maxSummaryChars = 300

type Service struct {
	store    Store
	signals  Signals
	rewriter Rewriter
}

func NewService(store Store, signals Signals, rewriter Rewriter) *Service {
	return &Service{store: store, signals: signals, rewriter: rewriter}
}

// Read returns the accepted profile as the scorer sees it. It satisfies
// articleindex.Preferences.
func (s *Service) Read(ctx context.Context) (string, error) {
	text, err := s.store.Current(ctx)
	if err != nil {
		return "", err
	}
	return ForModel(text), nil
}

// Current returns the accepted profile as stored, placeholders included.
func (s *Service) Current(ctx context.Context) (string, error) {
	return s.store.Current(ctx)
}

// Replace validates and stores a hand-edited profile.
func (s *Service) Replace(ctx context.Context, text string) (string, bool, error) {
	valid, err := Validate(text)
	if err != nil {
		return "", false, &InvalidError{err}
	}
	changed, err := s.store.Save(ctx, valid, SourceManual)
	return valid, changed, err
}

type Result struct {
	Profile string `json:"profile"`
	Changed bool   `json:"changed"`
	Signals int    `json:"signals"`
}

// Regenerate rewrites the accepted profile from the Reader's evidence. With no
// evidence it changes nothing and makes no model call, so a hand-seeded
// profile survives a cold start. Output that fails validation is rejected and
// the accepted profile stays in place.
func (s *Service) Regenerate(ctx context.Context) (Result, error) {
	current, err := s.store.Current(ctx)
	if err != nil {
		return Result{}, err
	}
	signals, err := s.signals.Signals(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("load signals: %w", err)
	}
	if len(signals) == 0 {
		return Result{Profile: current}, nil
	}

	signals = boundSignals(signals)
	proposed, err := s.rewriter.RegeneratePreferences(ctx, current, Evidence(signals))
	if err != nil {
		return Result{}, fmt.Errorf("regenerate: %w", err)
	}
	valid, err := Validate(proposed)
	if err != nil {
		return Result{}, fmt.Errorf("regenerated profile rejected: %w", err)
	}
	changed, err := s.store.Save(ctx, valid, SourceRegenerated)
	if err != nil {
		return Result{}, err
	}
	return Result{Profile: valid, Changed: changed, Signals: len(signals)}, nil
}

// boundSignals keeps every vote before any Open, then the newest Opens, up to
// maxSignals.
func boundSignals(signals []preferenceexport.Signal) []preferenceexport.Signal {
	sorted := append([]preferenceexport.Signal(nil), signals...)
	sort.SliceStable(sorted, func(i, j int) bool {
		a, b := sorted[i], sorted[j]
		if (a.Vote != "") != (b.Vote != "") {
			return a.Vote != ""
		}
		if a.OpenedAt == nil || b.OpenedAt == nil {
			return a.OpenedAt != nil
		}
		return a.OpenedAt.After(*b.OpenedAt)
	})
	if len(sorted) > maxSignals {
		sorted = sorted[:maxSignals]
	}
	return sorted
}

// Evidence renders signals as one line each, labelled by strength. An Article
// with both a vote and an Open is labelled by its vote: the Open adds nothing.
func Evidence(signals []preferenceexport.Signal) string {
	var b strings.Builder
	for _, sig := range signals {
		label := "opened (weak +)"
		switch sig.Vote {
		case "up":
			label = "voted up (strong +)"
		case "down":
			label = "voted down (strong -)"
		}
		fmt.Fprintf(&b, "- [%s] %s", label, sig.Title)
		if len(sig.Tags) > 0 {
			fmt.Fprintf(&b, " | tags: %s", strings.Join(sig.Tags, ", "))
		}
		if sig.SourceID != "" {
			fmt.Fprintf(&b, " | source: %s", sig.SourceID)
		}
		if summary := truncate(strings.Join(strings.Fields(sig.Summary), " "), maxSummaryChars); summary != "" {
			fmt.Fprintf(&b, " | summary: %s", summary)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "…"
}

// InvalidError wraps a validation failure so the handler can answer 400.
type InvalidError struct{ Err error }

func (e *InvalidError) Error() string { return e.Err.Error() }
func (e *InvalidError) Unwrap() error { return e.Err }
