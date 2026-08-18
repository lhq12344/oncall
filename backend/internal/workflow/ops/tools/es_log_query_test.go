package tools

import "testing"

func TestNormalizeESLogAction(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"":                    "query",
		"query":               "query",
		" search ":            "query",
		"find":                "query",
		"lookup":              "query",
		"discover_indices":    "discover_indices",
		"discover":            "discover_indices",
		"Discover Indices":    "discover_indices",
		"list-indices":        "discover_indices",
		"list_indexes":        "discover_indices",
		"indices":             "discover_indices",
		"index_discovery":     "discover_indices",
		"totally-unsupported": "",
	}

	for input, want := range cases {
		input := input
		want := want
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			if got := normalizeESLogAction(input); got != want {
				t.Fatalf("normalizeESLogAction(%q) = %q, want %q", input, got, want)
			}
		})
	}
}
