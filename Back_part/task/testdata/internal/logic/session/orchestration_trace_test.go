package session

import (
	"strings"
	"testing"
)

func TestClipTraceTextRemovesNewlinesAndClips(t *testing.T) {
	got := ClipTraceText("a\nb\tc")
	if strings.ContainsAny(got, "\n\t") {
		t.Fatalf("trace text was not normalized: %q", got)
	}

	long := strings.Repeat("中", 1300)
	got = ClipTraceText(long)
	if len([]rune(got)) > 1203 {
		t.Fatalf("trace text not clipped, len=%d", len([]rune(got)))
	}
}
