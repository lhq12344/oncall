package postgres

import "testing"

func TestPostgresStoreConstructsCaseStore(t *testing.T) {
	if NewStore("postgres://example") == nil {
		t.Fatal("expected store")
	}
}
