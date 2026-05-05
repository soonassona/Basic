package queue

import (
	"context"
	"sync"

	amqp "github.com/rabbitmq/amqp091-go"
)

// NoopPublisher records publishes in memory. Use it in unit tests for
// handlers that depend on the Publisher port but do not need a broker.
type NoopPublisher struct {
	mu       sync.Mutex
	Recorded []NoopPublish
}

type NoopPublish struct {
	Exchange   string
	RoutingKey string
	Body       []byte
	Headers    amqp.Table
}

func (p *NoopPublisher) Publish(_ context.Context, exchange, routingKey string, body []byte, headers amqp.Table) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Recorded = append(p.Recorded, NoopPublish{exchange, routingKey, body, headers})
	return nil
}
