package http

import (
	"encoding/json"
	"net/http"
	"time"

	"go_agent/internal/adapters/alertmanager"
	"go_agent/internal/evidence"
)

type IncidentWebhook struct {
	Ingress *evidence.Ingress
	Now     func() time.Time
}

func (h IncidentWebhook) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var alert alertmanager.Alert
	if err := json.NewDecoder(r.Body).Decode(&alert); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	now := time.Now().UTC()
	if h.Now != nil {
		now = h.Now()
	}
	ingress := h.Ingress
	if ingress == nil {
		ingress = evidence.NewIngress(0)
	}
	signal, accepted := ingress.Accept(alertmanager.ToIncidentSignal(alert), now)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"accepted": accepted, "fingerprint": signal.Fingerprint})
}
