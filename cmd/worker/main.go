package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/segmentio/kafka-go"

	"github.com/chun/kada-backend/config"
	"github.com/chun/kada-backend/internal/infra"
	"github.com/chun/kada-backend/internal/mq"
	"github.com/chun/kada-backend/internal/service"
)

// maxDeliveryAttempts is the maximum number of processing attempts for a single message.
// Beyond it the offset is committed and the message dropped: otherwise a poison message would
// block a single-partition single-consumer-group forever.
const maxDeliveryAttempts = 3

// isPermanentError reports non-retryable permanent errors (poison messages).
func isPermanentError(err error) bool {
	// Invalid JSON: retries always fail
	var syntaxErr *json.SyntaxError
	if errors.As(err, &syntaxErr) {
		return true
	}
	// Foreign key violation (23503): e.g. the link was deleted, retries always fail
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23503" {
		return true
	}
	return false
}

// attemptTracker tracks per-message processing attempts in memory (reset on restart, acceptable)
type attemptTracker struct {
	mu   sync.Mutex
	seen map[string]int
}

func newAttemptTracker() *attemptTracker {
	return &attemptTracker{seen: make(map[string]int)}
}

func (t *attemptTracker) record(topic string, partition int, offset int64) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	key := fmt.Sprintf("%s-%d-%d", topic, partition, offset)
	t.seen[key]++
	return t.seen[key]
}

func (t *attemptTracker) reset(topic string, partition int, offset int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.seen, fmt.Sprintf("%s-%d-%d", topic, partition, offset))
}

// ensureTopic idempotently creates the topic (numPartitions=1 / replicationFactor=1).
// It must be called before creating the reader: if the reader joins the consumer group
// while the topic is still being auto-created, kafka-go gets an empty assignment and
// hangs forever (segmentio/kafka-go#585).
func ensureTopic(ctx context.Context, brokerList []string, topic string) error {
	conn, err := kafka.DialContext(ctx, "tcp", brokerList[0])
	if err != nil {
		return err
	}
	defer conn.Close()

	controller, err := conn.Controller()
	if err != nil {
		return err
	}
	ctrlAddr := net.JoinHostPort(controller.Host, strconv.Itoa(controller.Port))
	ctrlConn, err := kafka.DialContext(ctx, "tcp", ctrlAddr)
	if err != nil {
		return err
	}
	defer ctrlConn.Close()

	return ctrlConn.CreateTopics(kafka.TopicConfig{
		Topic:             topic,
		NumPartitions:     1,
		ReplicationFactor: 1,
	})
}

// processClickMessage deserializes a click message and persists it
func processClickMessage(msg []byte, writer service.ClickWriter) error {
	var e mq.ClickEvent
	if err := json.Unmarshal(msg, &e); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return writer.WriteClick(ctx, e)
}

func main() {
	databaseURL := os.Getenv("DATABASE_URL")
	brokers := os.Getenv("KAFKA_BROKERS")
	topic := os.Getenv("KAFKA_TOPIC")
	if topic == "" {
		topic = "clicks"
	}
	brokerList := config.SplitBrokers(brokers)
	if len(brokerList) == 0 {
		log.Fatal("KAFKA_BROKERS is required for worker")
	}

	// Ensure the topic exists before joining the consumer group, avoiding the #585 empty-assignment hang.
	// On cold start the healthcheck may run before the controller is ready, so retry with backoff;
	// if it still fails, exit (otherwise we would keep getting an empty assignment and hang forever).
	var topicErr error
	for attempt := 1; attempt <= 10; attempt++ {
		topicCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		topicErr = ensureTopic(topicCtx, brokerList, topic)
		cancel()
		if topicErr == nil {
			break
		}
		// #nosec G706 -- topic comes from environment config and topicErr is an internal connection error, not user input
		log.Printf("ensure kafka topic %q failed (attempt %d/10): %v", topic, attempt, topicErr)
		time.Sleep(2 * time.Second)
	}
	if topicErr != nil {
		// #nosec G706 -- topic comes from environment config and topicErr is an internal connection error, not user input
		log.Fatalf("cannot ensure kafka topic %q exists: %v", topic, topicErr)
	}

	db, err := infra.NewDB(databaseURL)
	if err != nil {
		log.Fatalf("database connect failed: %v", err)
	}
	defer infra.CloseDB(db)
	store := service.NewClickStore(db)
	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers: brokerList,
		Topic:   topic,
		GroupID: "click-worker",
		// Keep MinBytes at 1: a single click event is about 200B, and a larger value would batch
		// and add up to MaxWait (10s) of consumer latency
		MinBytes: 1,
		MaxBytes: 10e6,
	})
	defer reader.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// #nosec G706 -- topic/brokers come from environment config, not user input
	log.Printf("click-worker consuming topic %q from %s", topic, brokers)
	tracker := newAttemptTracker()
	for {
		// Use FetchMessage instead of ReadMessage: ReadMessage commits offsets automatically,
		// so even a failed processing would be committed and the click lost forever. Here we
		// commit only after the write succeeds.
		m, err := reader.FetchMessage(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				log.Println("shutdown signal received")
				return
			}
			log.Printf("fetch message failed: %v", err)
			continue
		}

		procErr := processClickMessage(m.Value, store)
		if procErr == nil {
			tracker.reset(m.Topic, m.Partition, m.Offset)
			if err := reader.CommitMessages(ctx, m); err != nil {
				log.Printf("commit message failed: %v", err)
			}
			continue
		}

		attempts := tracker.record(m.Topic, m.Partition, m.Offset)
		if isPermanentError(procErr) || attempts >= maxDeliveryAttempts {
			// Poison message (invalid JSON / foreign key violation) or retry limit exceeded:
			// commit the offset and skip, so the single-partition consumer group is not
			// blocked forever by the same message.
			log.Printf("dropping click message after %d attempts (permanent error): %v", attempts, procErr)
			if err := reader.CommitMessages(ctx, m); err != nil {
				log.Printf("commit message failed: %v", err)
			}
			tracker.reset(m.Topic, m.Partition, m.Offset)
			continue
		}

		log.Printf("process message failed (attempt %d/%d): %v", attempts, maxDeliveryAttempts, procErr)
		time.Sleep(time.Second) // brief backoff to avoid busy spinning
	}
}
