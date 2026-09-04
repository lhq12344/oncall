package quality

type Status string

const (
	Pass       Status = "pass"
	Repairable Status = "repairable"
	Fail       Status = "fail"
)

type EvaluationResult struct {
	Status  Status
	Reasons []string
}

func ok() EvaluationResult { return EvaluationResult{Status: Pass} }
