package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/1084217636/linkgo-im/api"
	"github.com/1084217636/linkgo-im/internal/delivery"
	"github.com/1084217636/linkgo-im/internal/discovery"
	"github.com/1084217636/linkgo-im/internal/metrics"
	"github.com/redis/go-redis/v9"
	"github.com/segmentio/kafka-go"
	"google.golang.org/protobuf/proto"
)

type groupDispatchJob struct {
	Frame      *api.WireMessage `json:"frame"`
	Recipients []string         `json:"recipients"`
	Attempt    int              `json:"attempt"`
}

func main() {
	redisAddr := getEnv("REDIS_ADDR", "redis:6379")
	redisPassword := getEnv("REDIS_PASSWORD", "123456")
	kafkaBrokers := discovery.ParseEndpoints(getEnv("KAFKA_BROKERS", "kafka:9092"))
	kafkaTopic := getEnv("KAFKA_GROUP_TOPIC", "group_message_dispatch")
	retryTopic := getEnv("KAFKA_RETRY_TOPIC", "group_message_retry")
	dlqTopic := getEnv("KAFKA_DLQ_TOPIC", "group_message_dlq")
	consumerGroup := getEnv("KAFKA_CONSUMER_GROUP", "transfer-group")
	metricsPort := getEnv("TRANSFER_METRICS_PORT", "9102")

	rdb := redis.NewClient(&redis.Options{
		Addr:     redisAddr,
		Password: redisPassword,
		DB:       0,
	})
	defer rdb.Close()

	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers: kafkaBrokers,
		Topic:   kafkaTopic,
		GroupID: consumerGroup,
	})
	defer reader.Close()

	retryReader := kafka.NewReader(kafka.ReaderConfig{
		Brokers: kafkaBrokers,
		Topic:   retryTopic,
		GroupID: consumerGroup + "-retry",
	})
	defer retryReader.Close()

	retryWriter := &kafka.Writer{
		Addr:         kafka.TCP(kafkaBrokers...),
		Topic:        retryTopic,
		RequiredAcks: kafka.RequireOne,
		Balancer:     &kafka.Hash{},
	}
	defer retryWriter.Close()

	dlqWriter := &kafka.Writer{
		Addr:         kafka.TCP(kafkaBrokers...),
		Topic:        dlqTopic,
		RequiredAcks: kafka.RequireOne,
		Balancer:     &kafka.Hash{},
	}
	defer dlqWriter.Close()

	redisDelivery := &delivery.RedisDelivery{Rdb: rdb}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		mux := http.NewServeMux()
		mux.Handle("/metrics", metrics.Handler())
		if err := http.ListenAndServe(":"+metricsPort, mux); err != nil {
			log.Printf("transfer metrics server stopped: %v", err)
		}
	}()

	go consumeLoop(ctx, reader, retryWriter, dlqWriter, redisDelivery, false)
	log.Printf("[transfer] consuming topic=%s retry=%s dlq=%s", kafkaTopic, retryTopic, dlqTopic)
	consumeLoop(ctx, retryReader, retryWriter, dlqWriter, redisDelivery, true)
}

func consumeLoop(
	ctx context.Context,
	reader *kafka.Reader,
	retryWriter *kafka.Writer,
	dlqWriter *kafka.Writer,
	redisDelivery *delivery.RedisDelivery,
	isRetry bool,
) {
	for {
		msg, err := reader.ReadMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			metrics.KafkaOperations.WithLabelValues(stageLabel(isRetry), "read_error").Inc()
			log.Printf("read kafka message failed: %v", err)
			continue
		}

		var job groupDispatchJob
		if err := json.Unmarshal(msg.Value, &job); err != nil {
			metrics.KafkaOperations.WithLabelValues(stageLabel(isRetry), "decode_error").Inc()
			writeDeadLetter(ctx, dlqWriter, msg.Key, msg.Value)
			continue
		}
		if job.Frame == nil {
			metrics.KafkaOperations.WithLabelValues(stageLabel(isRetry), "empty").Inc()
			continue
		}

		payload, err := proto.Marshal(job.Frame)
		if err != nil {
			metrics.KafkaOperations.WithLabelValues(stageLabel(isRetry), "marshal_error").Inc()
			writeDeadLetter(ctx, dlqWriter, msg.Key, msg.Value)
			continue
		}

		deliveryErr := false
		for _, recipient := range job.Recipients {
			if err := redisDelivery.Deliver(ctx, recipient, job.Frame.MessageId, payload, job.Frame.SentAt); err != nil {
				deliveryErr = true
				log.Printf("deliver group message failed recipient=%s msg=%s: %v", recipient, job.Frame.MessageId, err)
				break
			}
		}

		if !deliveryErr {
			metrics.KafkaOperations.WithLabelValues(stageLabel(isRetry), "success").Inc()
			continue
		}

		job.Attempt++
		encoded, _ := json.Marshal(job)
		if job.Attempt <= 3 {
			if err := retryWriter.WriteMessages(ctx, kafka.Message{Key: msg.Key, Value: encoded}); err != nil {
				metrics.KafkaOperations.WithLabelValues("retry_write", "error").Inc()
				writeDeadLetter(ctx, dlqWriter, msg.Key, encoded)
				continue
			}
			metrics.KafkaOperations.WithLabelValues("retry_write", "success").Inc()
			continue
		}

		writeDeadLetter(ctx, dlqWriter, msg.Key, encoded)
	}
}

func writeDeadLetter(ctx context.Context, writer *kafka.Writer, key, value []byte) {
	if err := writer.WriteMessages(ctx, kafka.Message{Key: key, Value: value}); err != nil {
		metrics.KafkaOperations.WithLabelValues("dlq_write", "error").Inc()
		log.Printf("write dlq failed: %v", err)
		return
	}
	metrics.KafkaOperations.WithLabelValues("dlq_write", "success").Inc()
}

func stageLabel(isRetry bool) string {
	if isRetry {
		return "retry_consume"
	}
	return "consume"
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok && value != "" {
		return value
	}
	return fallback
}
