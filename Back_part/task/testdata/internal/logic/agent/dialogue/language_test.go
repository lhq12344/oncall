package dialogue

import (
	"context"
	"testing"

	"go_agent/internal/logic/ai/models"

	"github.com/cloudwego/eino/schema"
)

func TestDetectUserLanguageByRules(t *testing.T) {
	cases := []struct {
		name string
		text string
		want string
	}{
		{name: "english", text: "How do I reset my password?", want: "en"},
		{name: "japanese", text: "パスワードを変更したいです", want: "ja"},
		{name: "thai", text: "ฉันต้องการรีเซ็ตรหัสผ่าน", want: "th"},
		{name: "chinese", text: "我想修改密码", want: "zh"},
		{name: "mixed chinese english", text: "我 need help", want: "zh"},
		{name: "empty", text: "   ", want: ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := detectUserLanguageByRules(tc.text); got != tc.want {
				t.Fatalf("detectUserLanguageByRules(%q) = %q, want %q", tc.text, got, tc.want)
			}
		})
	}
}

func TestDetectUserLanguageFallbackToLLM(t *testing.T) {
	fakeModel := &fakeToolCallingChatModel{
		generateResponse: schema.AssistantMessage("ja", nil),
	}
	lang, err := detectUserLanguage(context.Background(), &models.ChatModel{Client: fakeModel}, "12345")
	if err != nil {
		t.Fatalf("detectUserLanguage returned error: %v", err)
	}
	if lang != "ja" {
		t.Fatalf("detectUserLanguage returned %q, want ja", lang)
	}
	if len(fakeModel.lastGenerate) != 2 {
		t.Fatalf("fake model received %d messages, want 2", len(fakeModel.lastGenerate))
	}
}

func TestDetectUserLanguageFallbackDefault(t *testing.T) {
	lang, err := detectUserLanguage(context.Background(), nil, "12345")
	if err == nil {
		t.Fatal("expected error from fallback language detection, got nil")
	}
	if lang != defaultUserLanguage {
		t.Fatalf("fallback language = %q, want %q", lang, defaultUserLanguage)
	}
}
