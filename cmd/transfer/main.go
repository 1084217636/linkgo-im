package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/1084217636/linkgo-im/api"
	"github.com/1084217636/linkgo-im/internal/delivery"
	"github.com/1084217636/linkgo-im/internal/discovery"
	"github.com/1084217636/linkgo-im/internal/health"
	"github.com/1084217636/linkgo-im/internal/metrics"
	"github.com/1084217636/linkgo-im/internal/server"
	"github.com/redis/go-redis/v9"
	"github.com/segmentio/kafka-go"
	"github.com/zeromicro/go-zero/core/logx"
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
	maxAttempts := getEnvInt("KAFKA_MAX_ATTEMPTS", 3)

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
		mux.HandleFunc("/healthz", health.LiveHandler())
		mux.HandleFunc("/readyz", health.ReadyHandler(map[string]health.Check{
			"redis": func(ctx context.Context) error {
				return rdb.Ping(ctx).Err()
			},
			"kafka": func(ctx context.Context) error {
				if len(kafkaBrokers) == 0 {
					return context.Canceled
				}
				conn, err := kafka.DialContext(ctx, "tcp", kafkaBrokers[0])
				if err != nil {
					return err
				}
				return conn.Close()
			},
		}))
		mux.Handle("/metrics", metrics.Handler())
		if err := http.ListenAndServe(":"+metricsPort, mux); err != nil {
			logx.Errorf("transfer metrics server stopped: %v", err)
		}
	}()

	go consumeLoop(ctx, reader, retryWriter, dlqWriter, rdb, redisDelivery, false, maxAttempts)
	logx.Infof("transfer consuming topic=%s retry=%s dlq=%s", kafkaTopic, retryTopic, dlqTopic)
	consumeLoop(ctx, retryReader, retryWriter, dlqWriter, rdb, redisDelivery, true, maxAttempts)
}

func consumeLoop(
	ctx context.Context,
	reader *kafka.Reader,
	retryWriter *kafka.Writer,
	dlqWriter *kafka.Writer,
	rdb *redis.Client,
	redisDelivery *delivery.RedisDelivery,
	isRetry bool,
	maxAttempts int,
) {
	for {
		msg, err := reader.ReadMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			metrics.KafkaOperations.WithLabelValues(stageLabel(isRetry), "read_error").Inc()
			logx.Errorf("read kafka message failed: %v", err)
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
		server.RememberSessionMessage(ctx, rdb, job.Frame, payload)

		deliveryErr := false
		var failedRecipient string
		for _, recipient := range job.Recipients {
			if err := deliverGroupRecipient(ctx, rdb, redisDelivery, recipient, job.Frame, payload); err != nil {
				deliveryErr = true
				failedRecipient = recipient
				logx.Errorw("deliver group message failed",
					logx.Field("trace_id", job.Frame.TraceId),
					logx.Field("message_id", job.Frame.MessageId),
					logx.Field("seq", job.Frame.Seq),
					logx.Field("target_id", recipient),
					logx.Field("attempt", job.Attempt),
					logx.Field("error", err.Error()),
				)
				break
			}
		}

		if !deliveryErr {
			metrics.KafkaOperations.WithLabelValues(stageLabel(isRetry), "success").Inc()
			logx.Infow("group dispatch consumed",
				logx.Field("trace_id", job.Frame.TraceId),
				logx.Field("message_id", job.Frame.MessageId),
				logx.Field("seq", job.Frame.Seq),
				logx.Field("target_id", job.Frame.To),
				logx.Field("recipient_count", len(job.Recipients)),
				logx.Field("retry", isRetry),
			)
			continue
		}

		job.Attempt++
		encoded, _ := json.Marshal(job)
		if job.Attempt <= maxAttempts {
			if err := retryWriter.WriteMessages(ctx, kafka.Message{Key: msg.Key, Value: encoded}); err != nil {
				metrics.KafkaOperations.WithLabelValues("retry_write", "error").Inc()
				writeDeadLetter(ctx, dlqWriter, msg.Key, encoded)
				continue
			}
			metrics.KafkaOperations.WithLabelValues("retry_write", "success").Inc()
			logx.Infow("group dispatch scheduled retry",
				logx.Field("trace_id", job.Frame.TraceId),
				logx.Field("message_id", job.Frame.MessageId),
				logx.Field("seq", job.Frame.Seq),
				logx.Field("target_id", failedRecipient),
				logx.Field("attempt", job.Attempt),
			)
			continue
		}

		logx.Errorw("group dispatch moved to dlq",
			logx.Field("trace_id", job.Frame.TraceId),
			logx.Field("message_id", job.Frame.MessageId),
			logx.Field("seq", job.Frame.Seq),
			logx.Field("target_id", failedRecipient),
			logx.Field("attempt", job.Attempt),
		)
		writeDeadLetter(ctx, dlqWriter, msg.Key, encoded)
	}
}

func deliverGroupRecipient(ctx context.Context, rdb *redis.Client, redisDelivery *delivery.RedisDelivery, recipient string, frame *api.WireMessage, payload []byte) error {
	key := groupRecipientDedupKey(frame.MessageId, recipient)
	locked, err := rdb.SetNX(ctx, key, "processing", 5*time.Minute).Result()
	if err != nil {
		return err
	}
	if !locked {
		metrics.KafkaOperations.WithLabelValues("dedupe", "skip").Inc()
		return nil
	}
	if err := redisDelivery.Deliver(ctx, recipient, frame.MessageId, payload, frame.SentAt); err != nil {
		_ = rdb.Del(ctx, key).Err()
		return err
	}
	return rdb.Set(ctx, key, "done", 7*24*time.Hour).Err()
}

func writeDeadLetter(ctx context.Context, writer *kafka.Writer, key, value []byte) {
	if err := writer.WriteMessages(ctx, kafka.Message{Key: key, Value: value}); err != nil {
		metrics.KafkaOperations.WithLabelValues("dlq_write", "error").Inc()
		logx.Errorf("write dlq failed: %v", err)
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

func getEnvInt(key string, fallback int) int {
	value := getEnv(key, "")
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func groupRecipientDedupKey(messageID, recipient string) string {
	return "group_delivery:" + messageID + ":" + recipient
}
