package dialogue

import (
	"context"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

func TestContextAwareModelInputRendersLanguageTemplate(t *testing.T) {
	ctx := withDialogueRuntimeContext(context.Background(), dialogueRuntimeContext{
		UserLanguage:   "th",
		SolvedContexts: []string{"中文资料A"},
	})

	msgs, err := contextAwareModelInput(ctx, answerAgentInstruction, &adk.AgentInput{
		Messages: []adk.Message{
			schema.UserMessage("hello"),
			schema.SystemMessage(analysisMessageMarker + "：intent_analysis={}"),
		},
	})
	if err != nil {
		t.Fatalf("contextAwareModelInput returned error: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("message count = %d, want 2", len(msgs))
	}
	if msgs[0].Role != schema.System {
		t.Fatalf("first role = %s, want %s", msgs[0].Role, schema.System)
	}
	if !strings.Contains(msgs[0].Content, analysisMessageMarker) {
		t.Fatalf("system prompt missing analysis marker: %s", msgs[0].Content)
	}
	if !strings.Contains(msgs[0].Content, "当前用户的提问语言是 th") {
		t.Fatalf("system prompt missing rendered language: %s", msgs[0].Content)
	}
	if !strings.Contains(msgs[0].Content, "中文资料A") {
		t.Fatalf("system prompt missing solved contexts: %s", msgs[0].Content)
	}
	if msgs[1].Role != schema.User {
		t.Fatalf("second role = %s, want %s", msgs[1].Role, schema.User)
	}
	if strings.Contains(msgs[1].Content, analysisMessageMarker) {
		t.Fatalf("user message should not contain analysis marker: %s", msgs[1].Content)
	}
}

func TestPlayerFacingPromptSuppressesRepeatedGreetingAndInternalNotes(t *testing.T) {
	ctx := withDialogueRuntimeContext(context.Background(), dialogueRuntimeContext{
		UserLanguage:   "zh",
		SolvedContexts: []string{"充值方式：GCash"},
	})

	msgs, err := contextAwareModelInput(ctx, answerAgentInstruction, &adk.AgentInput{
		Messages: []adk.Message{
			schema.UserMessage("GCash怎么充值？"),
		},
	})
	if err != nil {
		t.Fatalf("contextAwareModelInput returned error: %v", err)
	}
	if len(msgs) == 0 || msgs[0].Role != schema.System {
		t.Fatalf("first message should be system prompt: %#v", msgs)
	}

	prompt := msgs[0].Content
	required := []string{
		`回复要结合玩家本轮具体问题、已知项目、情绪和上下文个性化组织`,
		`标题和步骤名称应贴合玩家问题本身`,
		`后续轮次不要重复使用"您好，冒险者！"等固定问候`,
		`可以使用知识库和工具获得的信息回答玩家，但不要额外告诉玩家信息来源`,
		`可以直接使用 solved_contexts 中的知识回答玩家`,
		`不要向玩家显示任何内部标签、工具名称、分析字段、"来源：知识库检索结果"、"知识库说明"或"工具说明"`,
	}
	for _, want := range required {
		if !strings.Contains(prompt, want) {
			t.Fatalf("system prompt missing %q:\n%s", want, prompt)
		}
	}

	forbidden := []string{
		`明确注明"来源：知识库检索结果"`,
	}
	for _, text := range forbidden {
		if strings.Contains(prompt, text) {
			t.Fatalf("system prompt should not require %q:\n%s", text, prompt)
		}
	}
}
