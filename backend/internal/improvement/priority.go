package improvement

type PriorityInput struct {
	Frequency      int
	Severity       string
	AffectedScope  float64
	BusinessImpact float64
	ConfidenceGap  float64
	Recurrence     int
}

func Priority(input PriorityInput) float64 {
	severity := map[string]float64{"p0": 1000, "p1": 300, "p2": 100, "p3": 10}[input.Severity]
	if severity == 0 {
		severity = 5
	}
	return severity + float64(input.Frequency)*2 + input.AffectedScope + input.BusinessImpact + input.ConfidenceGap + float64(input.Recurrence)*3
}
