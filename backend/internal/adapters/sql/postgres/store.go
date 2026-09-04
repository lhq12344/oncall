package postgres

import "go_agent/internal/improvement"

// NewStore keeps the PostgreSQL seam available without claiming a live
// database connection. A real production deployment must replace this
// adapter with a PostgreSQL-backed implementation selected by configuration.
func NewStore(_ string) improvement.CaseStore { return improvement.NewMemoryCaseStore() }
