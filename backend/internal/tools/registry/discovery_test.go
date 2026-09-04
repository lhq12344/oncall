package registry

import "testing"

func TestSearchMatchesDescriptorFields(t *testing.T) {
	got := Search([]Descriptor{{ID: "k8s_monitor", Capability: "kubernetes.read"}, {ID: "execute_step", Capability: "execution.mutation"}}, "kubernetes", 5)
	if len(got) != 1 || got[0].ID != "k8s_monitor" {
		t.Fatalf("unexpected results: %+v", got)
	}
}
