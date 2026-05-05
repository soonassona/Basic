package queue

import (
	"context"

	amqp "github.com/rabbitmq/amqp091-go"
)

// JobPublisherAdapter wraps an *AMQPPublisher so it satisfies the
// application.JobPublisher port without leaking amqp091 types upward.
// The named-type difference between amqp.Table and map[string]any forces
// an explicit conversion — keeps the application layer dependency-free.
type JobPublisherAdapter struct {
	inner *AMQPPublisher
}

func NewJobPublisherAdapter(inner *AMQPPublisher) *JobPublisherAdapter {
	return &JobPublisherAdapter{inner: inner}
}

func (a *JobPublisherAdapter) Publish(ctx context.Context, exchange, routingKey string, body []byte, headers map[string]any) error {
	return a.inner.Publish(ctx, exchange, routingKey, body, amqp.Table(headers))
}
