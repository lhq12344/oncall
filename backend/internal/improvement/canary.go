package improvement

type CanaryScope struct {
	Tenant          string
	Project         string
	TrafficPercent  int
	RollbackVersion string
}

func (s CanaryScope) CanRollback() bool { return s.RollbackVersion != "" }
