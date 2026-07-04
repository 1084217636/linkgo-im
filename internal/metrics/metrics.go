package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	WSConnections = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "linkgo_ws_connections",
		Help: "Current active websocket connections.",
	})

	InboundMessages = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "linkgo_inbound_messages_total",
		Help: "Total inbound messages by source and type.",
	}, []string{"source", "type"})

	OutboundMessages = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "linkgo_outbound_messages_total",
		Help: "Total outbound messages by target and result.",
	}, []string{"target", "result"})

	AckOperations = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "linkgo_ack_operations_total",
		Help: "Total ack operations by result.",
	}, []string{"result"})

	KafkaOperations = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "linkgo_kafka_operations_total",
		Help: "Kafka operations by stage and result.",
	}, []string{"stage", "result"})

	RateLimitHits = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "linkgo_rate_limit_hits_total",
		Help: "Rate limit hits by route.",
	}, []string{"route"})

	RedPacketOperations = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "linkgo_red_packet_operations_total",
		Help: "Red packet operations by action and result.",
	}, []string{"action", "result"})

	AISummaryRequests = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "linkgo_ai_summary_requests_total",
		Help: "AI group summary requests by provider and result.",
	}, []string{"provider", "result"})
)

func Handler() http.Handler {
	return promhttp.Handler()
}
