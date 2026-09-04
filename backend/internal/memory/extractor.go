package memory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"time"
)

type CandidateKind string

const (
	KindUserPreference        CandidateKind = "user_preference"
	KindConfirmedEnvironment  CandidateKind = "confirmed_environment_fact"
	KindVerifiedOpsConclusion CandidateKind = "verified_ops_conclusion"
	KindRecurringConstraint   CandidateKind = "recurring_constraint"
)

type Candidate struct {
	Kind       CandidateKind
	Scope      string
	Owner      string
	Content    string
	Confidence float64
	Provenance string
	ExpiresAt  time.Time
}

type CompletedTurn struct {
	RunID   string
	TraceID string
	Text    string
}

type Extractor interface {
	Extract(context.Context, CompletedTurn) ([]Candidate, error)
}

func RecordFromCandidate(candidate Candidate, now time.Time) Record {
	sum := sha256.Sum256([]byte(candidate.Scope + "\n" + candidate.Content + "\n" + candidate.Provenance))
	return Record{ID: hex.EncodeToString(sum[:]), Source: string(candidate.Kind), Scope: candidate.Scope, Confidence: candidate.Confidence, Owner: candidate.Owner, CreatedAt: now, ExpiresAt: candidate.ExpiresAt, ContentHash: hex.EncodeToString(sum[:]), Provenance: candidate.Provenance, Content: candidate.Content}
}
