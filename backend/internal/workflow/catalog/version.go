package catalog

const CurrentIncidentVersion = "incident.workflow/v1"

func IncidentDefinition() Definition {
	return Definition{ID: IncidentWorkflow, Version: CurrentIncidentVersion, InputSchemaVersion: "incident.input/v1", StateSchemaVersion: "incident.state/v1", OutputSchemaVersion: "incident.output/v1", RequiredCapabilities: []string{"kubernetes.read", "metrics.read", "logs.read"}}
}
