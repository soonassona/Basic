//go:build integration

package queue_test

import (
	"context"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/visionloop/api/internal/infrastructure/queue"
)

// startRabbit boots a one-shot RabbitMQ 4 container and returns the AMQP
// URL. Cleanup is registered with t.Cleanup so concurrent tests get
// isolated brokers if the package later parallelises.
func startRabbit(ctx context.Context, t *testing.T) string {
	t.Helper()

	req := testcontainers.ContainerRequest{
		Image:        "rabbitmq:4.0-alpine",
		ExposedPorts: []string{"5672/tcp"},
		WaitingFor: wait.ForAll(
			wait.ForLog("Server startup complete"),
			wait.ForListeningPort("5672/tcp"),
		).WithDeadline(60 * time.Second),
	}
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("start rabbitmq: %v", err)
	}
	t.Cleanup(func() {
		_ = c.Terminate(context.Background())
	})

	host, err := c.Host(ctx)
	if err != nil {
		t.Fatalf("container host: %v", err)
	}
	port, err := c.MappedPort(ctx, "5672/tcp")
	if err != nil {
		t.Fatalf("container port: %v", err)
	}
	return "amqp://guest:guest@" + host + ":" + port.Port() + "/"
}

func dial(t *testing.T, url string) (*amqp.Connection, *amqp.Channel) {
	t.Helper()
	conn, err := amqp.Dial(url)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	ch, err := conn.Channel()
	if err != nil {
		t.Fatalf("channel: %v", err)
	}
	t.Cleanup(func() { _ = ch.Close() })
	return conn, ch
}

func TestDeclare_IsIdempotent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	url := startRabbit(ctx, t)
	_, ch := dial(t, url)

	if err := queue.Declare(ch); err != nil {
		t.Fatalf("first declare: %v", err)
	}
	if err := queue.Declare(ch); err != nil {
		t.Fatalf("second declare (must be no-op): %v", err)
	}
}

// TestPublishConsume_RoundTrip proves that a message published to the
// jobs exchange with routing key jobs.pending.auto lands in jobs.pending
// with persistent delivery mode.
func TestPublishConsume_RoundTrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	url := startRabbit(ctx, t)
	_, ch := dial(t, url)
	if err := queue.Declare(ch); err != nil {
		t.Fatalf("declare: %v", err)
	}

	body := []byte(`{"job_id":"00000000-0000-0000-0000-000000000001"}`)
	if err := ch.PublishWithContext(ctx, queue.ExchangeJobs, "jobs.pending.auto", false, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		Body:         body,
	}); err != nil {
		t.Fatalf("publish: %v", err)
	}

	deliveries, err := ch.Consume(queue.QueuePending, "test-consumer", false, false, false, false, nil)
	if err != nil {
		t.Fatalf("consume: %v", err)
	}

	select {
	case d := <-deliveries:
		if string(d.Body) != string(body) {
			t.Fatalf("body mismatch: got %q want %q", d.Body, body)
		}
		if d.DeliveryMode != amqp.Persistent {
			t.Fatalf("expected persistent delivery, got %d", d.DeliveryMode)
		}
		if d.RoutingKey != "jobs.pending.auto" {
			t.Fatalf("routing key: got %q want jobs.pending.auto", d.RoutingKey)
		}
		_ = d.Ack(false)
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for delivery")
	}
}

// TestNack_GoesToDeadLetter proves that a message rejected without
// requeue ends up in jobs.dead via the configured DLX. This is the
// reliability backbone called out in spec §11 ("Dead-letter queue
// after final retry exhaustion").
func TestNack_GoesToDeadLetter(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	url := startRabbit(ctx, t)
	_, ch := dial(t, url)
	if err := queue.Declare(ch); err != nil {
		t.Fatalf("declare: %v", err)
	}

	body := []byte(`{"job_id":"dlq-test"}`)
	if err := ch.PublishWithContext(ctx, queue.ExchangeJobs, "jobs.pending.box", false, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		Body:         body,
	}); err != nil {
		t.Fatalf("publish: %v", err)
	}

	// Consume from jobs.pending and reject without requeue.
	pending, err := ch.Consume(queue.QueuePending, "rejector", false, false, false, false, nil)
	if err != nil {
		t.Fatalf("consume pending: %v", err)
	}
	select {
	case d := <-pending:
		if err := d.Reject(false); err != nil {
			t.Fatalf("reject: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for pending delivery")
	}

	// Drain jobs.dead — message should appear via the DLX path.
	dead, err := ch.Consume(queue.QueueDead, "dead-consumer", false, false, false, false, nil)
	if err != nil {
		t.Fatalf("consume dead: %v", err)
	}
	select {
	case d := <-dead:
		if string(d.Body) != string(body) {
			t.Fatalf("dead body mismatch: got %q want %q", d.Body, body)
		}
		_ = d.Ack(false)
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for dead-letter")
	}
}

// TestRetryQueue_TTLRoutesBackToPending exercises option A from the plan:
// publish to jobs.retry with a short per-message TTL, then expect the
// message to dead-letter back into jobs.pending via the topic binding.
// This is the scaffolding the retry-policy task (separate TODO row) will
// drive.
func TestRetryQueue_TTLRoutesBackToPending(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	url := startRabbit(ctx, t)
	_, ch := dial(t, url)
	if err := queue.Declare(ch); err != nil {
		t.Fatalf("declare: %v", err)
	}

	body := []byte(`{"job_id":"retry-test","attempt":1}`)
	if err := ch.PublishWithContext(ctx, "", queue.QueueRetry, false, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		Expiration:   "200", // ms
		Body:         body,
	}); err != nil {
		t.Fatalf("publish to retry: %v", err)
	}

	pending, err := ch.Consume(queue.QueuePending, "retry-watcher", false, false, false, false, nil)
	if err != nil {
		t.Fatalf("consume pending: %v", err)
	}
	select {
	case d := <-pending:
		if string(d.Body) != string(body) {
			t.Fatalf("body mismatch after retry: got %q want %q", d.Body, body)
		}
		if d.RoutingKey != queue.RoutingPendingRe {
			t.Fatalf("routing key: got %q want %q", d.RoutingKey, queue.RoutingPendingRe)
		}
		_ = d.Ack(false)
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for retry message to re-arrive")
	}
}
