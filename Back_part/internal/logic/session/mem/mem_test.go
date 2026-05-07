package mem

import (
	"fmt"
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"
)

func TestCompactStoredTurns_CompactsOldestTwentyWhenAboveForty(t *testing.T) {
	turns := make([]storedTurn, 0, 41)
	for i := 1; i <= 41; i++ {
		turns = append(turns, storedTurn{
			T: 10,
			Msgs: []*schema.Message{
				schema.UserMessage(fmt.Sprintf("问题-%d", i)),
				schema.AssistantMessage(fmt.Sprintf("回复-%d", i), nil),
			},
		})
	}

	summary, remaining, changed := compactStoredTurns("", turns, 20, 40, 4000)
	if !changed {
		t.Fatalf("expected compaction to happen")
	}
	if len(remaining) != 21 {
		t.Fatalf("expected 21 raw turns remaining, got %d", len(remaining))
	}
	if summary == "" {
		t.Fatalf("expected summary to be generated")
	}
	if !strings.Contains(summary, "问题-1") {
		t.Fatalf("expected oldest compacted turn to be summarized")
	}
	if strings.Contains(summary, "问题-21") {
		t.Fatalf("expected turn 21 to remain raw instead of entering summary")
	}
}

func TestCompactStoredTurns_MergesExistingSummary(t *testing.T) {
	turns := []storedTurn{
		{
			T: 10,
			Msgs: []*schema.Message{
				schema.UserMessage("新问题-1"),
				schema.AssistantMessage("新回复-1", nil),
			},
		},
		{
			T: 10,
			Msgs: []*schema.Message{
				schema.UserMessage("新问题-2"),
				schema.AssistantMessage("新回复-2", nil),
			},
		},
	}

	summary, remaining, changed := compactStoredTurns("旧摘要", turns, 1, 1, 4000)
	if !changed {
		t.Fatalf("expected compaction to happen")
	}
	if len(remaining) != 1 {
		t.Fatalf("expected 1 turn remaining, got %d", len(remaining))
	}
	if !strings.Contains(summary, "旧摘要") {
		t.Fatalf("expected existing summary to be kept")
	}
	if !strings.Contains(summary, "新问题-1") {
		t.Fatalf("expected new compacted turns to merge into summary")
	}
}
