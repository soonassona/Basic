package queue

import (
	"context"
	"fmt"

	amqp "github.com/rabbitmq/amqp091-go"
)

// Publisher is the port the application layer sees. Phase 2's POST /v1/jobs
// will depend on this interface, not on amqp091 directly, so handlers stay
// testable without a broker.
type Publisher interface {
	Publish(ctx context.Context, exchange, routingKey string, body []byte, headers amqp.Table) error
}

// AMQPPublisher publishes through the long-lived channel held by Connection.
// Persistent delivery is forced: messages survive broker restart, in line
// with spec §11 ("Persistent message delivery mode").
type AMQPPublisher struct {
	conn *Connection
}

func NewAMQPPublisher(conn *Connection) *AMQPPublisher {
	return &AMQPPublisher{conn: conn}
}

func (p *AMQPPublisher) Publish(ctx context.Context, exchange, routingKey string, body []byte, headers amqp.Table) error {
	ch := p.conn.Channel()
	if ch == nil {
		return fmt.Errorf("publish %s: no channel", routingKey)
	}
	pubCtx, cancel := context.WithTimeout(ctx, p.conn.PublishTimeout())
	defer cancel()

	return ch.PublishWithContext(
		pubCtx,
		exchange,
		routingKey,
		false, // mandatory — set true once we wire returns; off by default to avoid surprises pre-handler
		false, // immediate — deprecated
		amqp.Publishing{
			ContentType:  "application/json",
			DeliveryMode: amqp.Persistent,
			Headers:      headers,
			Body:         body,
		},
	)
}
