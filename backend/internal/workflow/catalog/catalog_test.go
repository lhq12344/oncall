package catalog

import "testing"

func TestCatalogResolvesIncidentDefinition(t *testing.T) {
	def, err := Default().Resolve(IncidentWorkflow, CurrentIncidentVersion)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if def.StateSchemaVersion == "" || len(def.RequiredCapabilities) == 0 {
		t.Fatalf("incomplete definition: %+v", def)
	}
}

func TestMigrationRejectsUnknownVersion(t *testing.T) {
	if _, err := MigrateStateVersion("old", "new", []byte("{}")); err == nil {
		t.Fatal("expected migration error")
	}
}
