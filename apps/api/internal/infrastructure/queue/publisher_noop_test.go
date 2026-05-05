package queue_test

import (
	"context"
	"testing"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/visionloop/api/internal/infrastructure/queue"
)

func TestNoopPublisher_RecordsPublishes(t *testing.T) {
	t.Parallel()
	p := &queue.NoopPublisher{}

	if err := p.Publish(context.Background(), queue.ExchangeJobs, "jobs.pending.auto", []byte("a"), amqp.Table{"x": 1}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if err := p.Publish(context.Background(), queue.ExchangeJobs, "jobs.pending.box", []byte("b"), nil); err != nil {
		t.Fatalf("publish: %v", err)
	}

	if got := len(p.Recorded); got != 2 {
		t.Fatalf("recorded count: got %d want 2", got)
	}
	if p.Recorded[0].RoutingKey != "jobs.pending.auto" || string(p.Recorded[0].Body) != "a" {
		t.Fatalf("first record wrong: %+v", p.Recorded[0])
	}
	if p.Recorded[1].RoutingKey != "jobs.pending.box" || string(p.Recorded[1].Body) != "b" {
		t.Fatalf("second record wrong: %+v", p.Recorded[1])
	}
}
