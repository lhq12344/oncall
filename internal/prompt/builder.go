package prompt

import (
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"time"
)

type Role string

const (
	RoleDialogue  Role = "dialogue"
	RoleRCA       Role = "rca"
	RoleOps       Role = "ops"
	RolePlan      Role = "plan"
	RoleExecution Role = "execution"
	RoleStrategy  Role = "strategy"
)

type Section struct {
	Name     string
	Priority int
	Content  string
}

type EnvironmentContext struct {
	WorkDir   string
	OS        string
	Arch      string
	Shell     string
	IsGitRepo bool
	GitBranch string
	Model     string
	Date      string
}

type BuildOptions struct {
	CustomInstructions string
	KnowledgeSection   string
	MemorySection      string
}

type Builder struct {
	sections []Section
}

func NewBuilder() *Builder {
	return &Builder{}
}

func (b *Builder) Add(s Section) *Builder {
	b.sections = append(b.sections, s)
	return b
}

func (b *Builder) Build() string {
	sort.SliceStable(b.sections, func(i, j int) bool {
		return b.sections[i].Priority < b.sections[j].Priority
	})

	parts := make([]string, 0, len(b.sections))
	for _, s := range b.sections {
		content := strings.TrimSpace(s.Content)
		if content != "" {
			parts = append(parts, content)
		}
	}

	return strings.Join(parts, "\n\n")
}

func DetectEnvironment(workDir string) EnvironmentContext {
	workDir = strings.TrimSpace(workDir)
	if workDir == "" {
		if wd, err := os.Getwd(); err == nil {
			workDir = wd
		} else {
			workDir = "."
		}
	}

	env := EnvironmentContext{
		WorkDir: workDir,
		OS:      runtime.GOOS,
		Arch:    runtime.GOARCH,
		Shell:   detectShell(),
		Date:    time.Now().Format("2006-01-02"),
	}

	if out, err := exec.Command("git", "-C", workDir, "rev-parse", "--is-inside-work-tree").Output(); err == nil && strings.TrimSpace(string(out)) == "true" {
		env.IsGitRepo = true
		if branch, err := exec.Command("git", "-C", workDir, "rev-parse", "--abbrev-ref", "HEAD").Output(); err == nil {
			env.GitBranch = strings.TrimSpace(string(branch))
		}
	}

	return env
}

func BuildSystemPrompt(env EnvironmentContext, opts BuildOptions) string {
	b := NewBuilder()
	addBaseSections(b)
	addContextSections(b, env, opts)
	return b.Build()
}

func BuildAgentPrompt(role Role, env EnvironmentContext, opts BuildOptions) string {
	b := NewBuilder()
	addBaseSections(b)
	if guidance := strings.TrimSpace(DeferredToolGuidance(role)); guidance != "" {
		b.Add(Section{Name: "DeferredToolGuidance", Priority: 45, Content: guidance})
	}
	b.Add(RoleSection(role))
	addContextSections(b, env, opts)
	return b.Build()
}

func addBaseSections(b *Builder) {
	b.Add(IdentitySection())
	b.Add(SystemSection())
	b.Add(TaskExecutionSection())
	b.Add(ExecutingActionsSection())
	b.Add(ToolUseSection())
	b.Add(ToneStyleSection())
	b.Add(OutputEfficiencySection())
}

func addContextSections(b *Builder, env EnvironmentContext, opts BuildOptions) {
	b.Add(EnvironmentSection(env))
	if opts.CustomInstructions != "" {
		b.Add(Section{Name: "CustomInstructions", Priority: 80, Content: "# 项目自定义指令\n" + opts.CustomInstructions})
	}
	if opts.KnowledgeSection != "" {
		b.Add(Section{Name: "Knowledge", Priority: 85, Content: "# 知识补充\n" + opts.KnowledgeSection})
	}
	if opts.MemorySection != "" {
		b.Add(Section{Name: "Memory", Priority: 95, Content: "# 长期记忆\n" + opts.MemorySection})
	}
}

func detectShell() string {
	if shell := strings.TrimSpace(os.Getenv("SHELL")); shell != "" {
		return shell
	}
	if shell := strings.TrimSpace(os.Getenv("COMSPEC")); shell != "" {
		return shell
	}
	if runtime.GOOS == "windows" {
		return "powershell"
	}
	return "sh"
}
