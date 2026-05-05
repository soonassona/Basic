package queue

import (
	"context"
	"fmt"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// Connection wraps an *amqp.Connection plus a single long-lived publish
// channel. amqp091 connections are goroutine-safe but channels are not,
// so concurrent publishers serialise through the connection's internal
// mutex.
//
// The reconnect loop is intentionally minimal: on close it retries with
// exponential backoff (capped at 30s). Phase 6 will replace the bare
// log calls with OpenTelemetry events.
type Connection struct {
	url            string
	dialTimeout    time.Duration
	publishTimeout time.Duration

	mu     sync.RWMutex
	conn   *amqp.Connection
	pubCh  *amqp.Channel
	closed chan struct{}
}

// Dial opens a connection and a publish channel, then runs Declare.
// Returns an error if any of those fail; callers should fail boot.
func Dial(ctx context.Context, url string, dialTimeout, publishTimeout time.Duration) (*Connection, error) {
	c := &Connection{
		url:            url,
		dialTimeout:    dialTimeout,
		publishTimeout: publishTimeout,
		closed:         make(chan struct{}),
	}
	if err := c.open(ctx); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Connection) open(ctx context.Context) error {
	dialCtx, cancel := context.WithTimeout(ctx, c.dialTimeout)
	defer cancel()

	type dialResult struct {
		conn *amqp.Connection
		err  error
	}
	done := make(chan dialResult, 1)
	go func() {
		conn, err := amqp.Dial(c.url)
		done <- dialResult{conn, err}
	}()

	var conn *amqp.Connection
	select {
	case <-dialCtx.Done():
		return fmt.Errorf("dial rabbitmq: %w", dialCtx.Err())
	case r := <-done:
		if r.err != nil {
			return fmt.Errorf("dial rabbitmq: %w", r.err)
		}
		conn = r.conn
	}

	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("open channel: %w", err)
	}
	if err := Declare(ch); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return err
	}

	c.mu.Lock()
	c.conn = conn
	c.pubCh = ch
	c.mu.Unlock()
	return nil
}

// Channel returns the long-lived publish channel. Callers should not
// close it; use Close on the Connection instead.
func (c *Connection) Channel() *amqp.Channel {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.pubCh
}

// PublishTimeout exposes the configured per-publish deadline so the
// Publisher can apply it without re-reading config.
func (c *Connection) PublishTimeout() time.Duration {
	return c.publishTimeout
}

// Close shuts the channel and connection down. Safe to call once.
func (c *Connection) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	select {
	case <-c.closed:
		return nil
	default:
		close(c.closed)
	}
	var firstErr error
	if c.pubCh != nil {
		if err := c.pubCh.Close(); err != nil {
			firstErr = err
		}
	}
	if c.conn != nil && !c.conn.IsClosed() {
		if err := c.conn.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
