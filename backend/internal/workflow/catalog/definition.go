package catalog

type WorkflowID string

const (
	IncidentWorkflow WorkflowID = "incident_workflow"
	ChangeWorkflow   WorkflowID = "change_workflow"
)

type Definition struct {
	ID                   WorkflowID
	Version              string
	InputSchemaVersion   string
	StateSchemaVersion   string
	OutputSchemaVersion  string
	RequiredCapabilities []string
}
