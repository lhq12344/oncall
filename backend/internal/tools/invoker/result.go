package invoker

type ToolResult struct {
	Output      string
	IsError     bool
	ArtifactRef string
}

func Error(output string) ToolResult { return ToolResult{Output: output, IsError: true} }
