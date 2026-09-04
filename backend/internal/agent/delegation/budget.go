package delegation

func AllowsTools(parent, child []string) bool {
	allowed := map[string]bool{}
	for _, tool := range parent {
		allowed[tool] = true
	}
	for _, tool := range child {
		if !allowed[tool] {
			return false
		}
	}
	return true
}

func WithinBudget(task Task, tokens, tools int) bool {
	if task.TokenBudget > 0 && tokens > task.TokenBudget {
		return false
	}
	if task.ToolBudget > 0 && tools > task.ToolBudget {
		return false
	}
	return true
}
