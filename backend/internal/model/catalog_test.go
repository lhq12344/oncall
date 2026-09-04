package model

import "testing"

func TestCatalogResolveDefaultRole(t *testing.T) {
	c, err := NewCatalog([]Profile{{ID: "fast", Role: "chat", Default: true}, {ID: "embed", Role: "embedding", Default: true}})
	if err != nil {
		t.Fatalf("NewCatalog: %v", err)
	}
	p, ok := c.Resolve("chat")
	if !ok || p.ID != "fast" {
		t.Fatalf("Resolve(chat) = %+v, %v", p, ok)
	}
}

func TestCatalogRejectsDuplicateDefaults(t *testing.T) {
	_, err := NewCatalog([]Profile{{ID: "a", Role: "chat", Default: true}, {ID: "b", Role: "chat", Default: true}})
	if err == nil {
		t.Fatal("expected duplicate default error")
	}
}

func TestCatalogRequireCapability(t *testing.T) {
	c := DefaultCatalog()
	if _, err := c.RequireCapability(nil, RoleDialogue, "streaming"); err != nil {
		t.Fatalf("RequireCapability(streaming): %v", err)
	}
	if _, err := c.RequireCapability(nil, RoleDialogue, "vision"); err == nil {
		t.Fatal("expected missing vision capability error")
	}
}
