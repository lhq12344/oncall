package evidence

import "time"

type Query struct {
	Scope       Scope
	Since       time.Duration
	NeedK8s     bool
	NeedLogs    bool
	NeedMetrics bool
	NeedHistory bool
}
