package dialogue

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode"

	"go_agent/internal/logic/ai/models"

	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

const (
	defaultUserLanguage     = "zh"
	languageDetectorTimeout = 3 * time.Second
)

type dialogueRuntimeContextKey struct{}

type dialogueRuntimeContext struct {
	UserLanguage     string
	SolvedContexts   []string
	PendingQuestions []string
}

func withDialogueRuntimeContext(ctx context.Context, runtime dialogueRuntimeContext) context.Context {
	return context.WithValue(ctx, dialogueRuntimeContextKey{}, runtime)
}

func dialogueRuntimeContextFromContext(ctx context.Context) dialogueRuntimeContext {
	runtime, _ := ctx.Value(dialogueRuntimeContextKey{}).(dialogueRuntimeContext)
	return runtime
}

func detectUserLanguage(ctx context.Context, llm *models.ChatModel, question string) (string, error) {
	if lang := detectUserLanguageByRules(question); lang != "" {
		return lang, nil
	}
	if llm == nil || llm.Client == nil {
		return defaultUserLanguage, fmt.Errorf("language detection fallback unavailable")
	}

	detectCtx, cancel := context.WithTimeout(ctx, languageDetectorTimeout)
	defer cancel()

	sysMsg := schema.SystemMessage(
		`识别用户问题的主要语言，只输出一个小写语言代码。` +
			`常见输出：zh、en、ja、th、ko。` +
			`如果无法确定，也只输出 zh。`,
	)
	userMsg := schema.UserMessage(strings.TrimSpace(question))

	output, err := llm.Client.Generate(detectCtx, []*schema.Message{sysMsg, userMsg})
	if err != nil {
		return defaultUserLanguage, err
	}

	lang := normalizeLanguageCode(output.Content)
	if lang == "" {
		return defaultUserLanguage, fmt.Errorf("language detector returned empty code")
	}
	return lang, nil
}

func detectUserLanguageByRules(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}

	var latinCount int
	var hanCount int
	var thaiCount int
	var hiraganaKatakanaCount int
	var hangulCount int

	for _, r := range text {
		switch {
		case unicode.In(r, unicode.Hiragana, unicode.Katakana):
			hiraganaKatakanaCount++
		case unicode.In(r, unicode.Han):
			hanCount++
		case unicode.In(r, unicode.Thai):
			thaiCount++
		case unicode.In(r, unicode.Hangul):
			hangulCount++
		case unicode.In(r, unicode.Latin):
			if unicode.IsLetter(r) {
				latinCount++
			}
		}
	}

	switch {
	case thaiCount > 0:
		return "th"
	case hiraganaKatakanaCount > 0:
		return "ja"
	case hangulCount > 0:
		return "ko"
	case hanCount > 0:
		return "zh"
	case latinCount > 0:
		return "en"
	default:
		return ""
	}
}

func normalizeLanguageCode(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ""
	}

	replacer := strings.NewReplacer("\n", " ", "\r", " ", "\t", " ", ".", " ", ",", " ", ":", " ")
	value = replacer.Replace(value)
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return ""
	}

	switch fields[0] {
	case "zh", "zh-cn", "zh-hans", "chinese":
		return "zh"
	case "en", "en-us", "english":
		return "en"
	case "ja", "ja-jp", "jp", "japanese":
		return "ja"
	case "th", "th-th", "thai":
		return "th"
	case "ko", "ko-kr", "korean":
		return "ko"
	default:
		if len(fields[0]) == 2 {
			return fields[0]
		}
		return ""
	}
}

func applyTerminology(text string, lang string) string {
	_ = lang
	return text
}

func buildPromptTemplateValues(ctx context.Context) map[string]any {
	runtime := dialogueRuntimeContextFromContext(ctx)
	values := map[string]any{
		"UserLanguage":          defaultUserLanguage,
		"SolvedContextsText":    "（无）",
		"PendingQuestionsText":  "（无）",
		"SolvedContextsCount":   0,
		"PendingQuestionsCount": 0,
	}

	if lang := normalizeLanguageCode(runtime.UserLanguage); lang != "" {
		values["UserLanguage"] = lang
	}
	if len(runtime.SolvedContexts) > 0 {
		values["SolvedContextsText"] = joinContextLines(runtime.SolvedContexts)
		values["SolvedContextsCount"] = len(runtime.SolvedContexts)
	}
	if len(runtime.PendingQuestions) > 0 {
		values["PendingQuestionsText"] = joinBulletList(runtime.PendingQuestions)
		values["PendingQuestionsCount"] = len(runtime.PendingQuestions)
	}
	return values
}

func joinBulletList(items []string) string {
	if len(items) == 0 {
		return "（无）"
	}
	lines := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		lines = append(lines, "- "+item)
	}
	if len(lines) == 0 {
		return "（无）"
	}
	return strings.Join(lines, "\n")
}

func joinContextLines(items []string) string {
	if len(items) == 0 {
		return "（无）"
	}
	lines := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		lines = append(lines, item)
	}
	if len(lines) == 0 {
		return "（无）"
	}
	return strings.Join(lines, "\n---\n")
}

func userLanguageFromState(ctx context.Context) string {
	runtime := dialogueRuntimeContextFromContext(ctx)
	if lang := normalizeLanguageCode(runtime.UserLanguage); lang != "" {
		return lang
	}

	var language string
	_ = composeProcessState(ctx, func(state *OrchState) {
		language = normalizeLanguageCode(state.UserLanguage)
	})
	if language != "" {
		return language
	}
	return defaultUserLanguage
}

func withDialogueRuntimeContextFromState(ctx context.Context) context.Context {
	runtime := dialogueRuntimeContextFromContext(ctx)
	_ = composeProcessState(ctx, func(state *OrchState) {
		runtime.UserLanguage = normalizeLanguageCode(state.UserLanguage)
		runtime.SolvedContexts = append([]string(nil), state.SolvedContexts...)
		runtime.PendingQuestions = append([]string(nil), state.PendingQuestions...)
	})
	if runtime.UserLanguage == "" {
		runtime.UserLanguage = defaultUserLanguage
	}
	return withDialogueRuntimeContext(ctx, runtime)
}

func composeProcessState(ctx context.Context, fn func(state *OrchState)) error {
	return compose.ProcessState[*OrchState](ctx, func(_ context.Context, state *OrchState) error {
		if state != nil {
			fn(state)
		}
		return nil
	})
}
