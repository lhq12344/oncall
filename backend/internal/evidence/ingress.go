package evidence

import (
	"sync"
	"time"
)

type Ingress struct {
	Window time.Duration
	mu     sync.Mutex
	seen   map[string]time.Time
}

func NewIngress(window time.Duration) *Ingress {
	if window <= 0 {
		window = 10 * time.Minute
	}
	return &Ingress{Window: window, seen: map[string]time.Time{}}
}

func (i *Ingress) Accept(signal IncidentSignal, now time.Time) (IncidentSignal, bool) {
	if i == nil {
		i = NewIngress(0)
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	signal = signal.Normalized()
	i.mu.Lock()
	defer i.mu.Unlock()
	if previous, ok := i.seen[signal.Fingerprint]; ok && now.Sub(previous) < i.Window {
		return signal, false
	}
	i.seen[signal.Fingerprint] = now
	return signal, true
}
