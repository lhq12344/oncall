package delegation

type Status string

const (
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
	StatusTimeout   Status = "timeout"
	StatusCancelled Status = "cancelled"
)

type Result struct {
	TaskID       string
	Status       Status
	Output       string
	ArtifactRefs []string
	Error        string
}
