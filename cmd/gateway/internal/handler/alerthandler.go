package handler

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"os"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/zeromicro/go-zero/core/logx"
)

var alertsReceived = promauto.NewCounterVec(prometheus.CounterOpts{Name: "linkgo_alert_webhooks_total", Help: "Authenticated local alert deliveries by state."}, []string{"state"})

// Local demo receiver: validates the configured Alertmanager identity and audits
// delivery. It does not send email or messages to external people.
func AlertHandler(w http.ResponseWriter, r *http.Request) {
	expected := os.Getenv("ALERT_WEBHOOK_TOKEN")
	if expected == "" || subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+expected)) != 1 {
		http.Error(w, "unauthorized", 401)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	var body struct {
		Status string `json:"status"`
		Alerts []struct {
			Status string            `json:"status"`
			Labels map[string]string `json:"labels"`
		} `json:"alerts"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || (body.Status != "firing" && body.Status != "resolved") {
		http.Error(w, "invalid alert", 400)
		return
	}
	for _, alert := range body.Alerts {
		logx.Infow("local alert received", logx.Field("alert_name", alert.Labels["alertname"]), logx.Field("alert_state", alert.Status))
		alertsReceived.WithLabelValues(body.Status).Inc()
	}
	w.WriteHeader(http.StatusNoContent)
}
