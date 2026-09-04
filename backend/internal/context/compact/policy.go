package compact

type Policy struct {
	MaxTokens         int
	TailTokens        int
	MaxAutoRetries    int
	PreserveToolPairs bool
}

func DefaultPolicy(maxTokens int) Policy {
	if maxTokens <= 0 {
		maxTokens = 8192
	}
	return Policy{MaxTokens: maxTokens, TailTokens: maxTokens / 3, MaxAutoRetries: 1, PreserveToolPairs: true}
}
