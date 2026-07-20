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

	PushQueueSubmissions = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "linkgo_push_queue_submissions_total",
		Help: "Push worker pool submissions by result.",
	}, []string{"result"})

	PushQueueDepth = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "linkgo_push_queue_depth",
		Help: "Current queued push tasks by shard.",
	}, []string{"shard"})

	PushProcessingLatencySeconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "linkgo_push_processing_latency_seconds",
		Help:    "Push task processing latency by result.",
		Buckets: prometheus.DefBuckets,
	}, []string{"result"})

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

	AIAskRequests = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "linkgo_ai_ask_requests_total",
		Help: "AI knowledge ask requests by provider and result.",
	}, []string{"provider", "result"})

	AIAskKnowledgeHits = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "linkgo_ai_ask_knowledge_hits",
		Help:    "Knowledge document hits per AI ask request by provider.",
		Buckets: []float64{0, 1, 2, 3, 5, 8},
	}, []string{"provider"})

	AIProviderLatencySeconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "linkgo_ai_provider_latency_seconds",
		Help:    "AI provider call latency in seconds by provider and result.",
		Buckets: []float64{0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10, 30},
	}, []string{"provider", "result"})

	GameOpsOperations = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "linkgo_gameops_operations_total",
		Help: "Game operations control-plane requests by operation and result.",
	}, []string{"operation", "result"})

	GameOpsOperationLatencySeconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "linkgo_gameops_operation_latency_seconds",
		Help:    "Game operations control-plane request latency by operation.",
		Buckets: prometheus.DefBuckets,
	}, []string{"operation"})

	GameOpsGrantedItems = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "linkgo_gameops_granted_items_total",
		Help: "Item grant entries handled by result; retries do not increment success.",
	}, []string{"result"})

	GameOpsCacheSync = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "linkgo_gameops_cache_sync_total",
		Help: "Activity cache synchronization attempts by result.",
	}, []string{"result"})
)

func Handler() http.Handler {
	return promhttp.Handler()
}
