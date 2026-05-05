package queue

import (
	"fmt"

	amqp "github.com/rabbitmq/amqp091-go"
)

// Topology names. These are referenced by the publisher, the retry policy
// (spec §11), and the AI-service consumer. Changing a name here is a
// breaking change to the wire contract — bump ADR-0004 if you do.
const (
	ExchangeJobs    = "jobs"
	ExchangeJobsDLX = "jobs.dlx"

	QueuePending = "jobs.pending"
	QueueRetry   = "jobs.retry"
	QueueDead    = "jobs.dead"

	RoutingPendingAll = "jobs.pending.*"
	RoutingPendingRe  = "jobs.pending.retry"
	RoutingDead       = "jobs.dead"
)

// Declare creates exchanges, queues, and bindings. It is idempotent —
// re-running against an already-declared broker is a no-op as long as
// the existing definitions match. RabbitMQ returns a channel error
// (PRECONDITION_FAILED) if arguments diverge, which is what we want:
// we'd rather fail boot than silently bind to a queue with the wrong
// dead-letter target.
func Declare(ch *amqp.Channel) error {
	exchanges := []struct {
		name string
		kind string
	}{
		{ExchangeJobs, "topic"},
		{ExchangeJobsDLX, "topic"},
	}
	for _, e := range exchanges {
		if err := ch.ExchangeDeclare(e.name, e.kind, true, false, false, false, nil); err != nil {
			return fmt.Errorf("declare exchange %s: %w", e.name, err)
		}
	}

	if _, err := ch.QueueDeclare(QueuePending, true, false, false, false, amqp.Table{
		"x-dead-letter-exchange":    ExchangeJobsDLX,
		"x-dead-letter-routing-key": RoutingDead,
	}); err != nil {
		return fmt.Errorf("declare queue %s: %w", QueuePending, err)
	}
	if err := ch.QueueBind(QueuePending, RoutingPendingAll, ExchangeJobs, false, nil); err != nil {
		return fmt.Errorf("bind queue %s: %w", QueuePending, err)
	}

	if _, err := ch.QueueDeclare(QueueRetry, true, false, false, false, amqp.Table{
		"x-dead-letter-exchange":    ExchangeJobs,
		"x-dead-letter-routing-key": RoutingPendingRe,
	}); err != nil {
		return fmt.Errorf("declare queue %s: %w", QueueRetry, err)
	}
	// jobs.retry has no binding — the retry publisher addresses it
	// directly via the default exchange with a per-message TTL.

	if _, err := ch.QueueDeclare(QueueDead, true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare queue %s: %w", QueueDead, err)
	}
	if err := ch.QueueBind(QueueDead, RoutingDead, ExchangeJobsDLX, false, nil); err != nil {
		return fmt.Errorf("bind queue %s: %w", QueueDead, err)
	}

	return nil
}
