package eventbus_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/visionloop/api/internal/application"
	"github.com/visionloop/api/internal/infrastructure/eventbus"
)

func TestHub_PublishReachesEverySubscriber(t *testing.T) {
	t.Parallel()
	h := eventbus.NewMemoryHub()
	jobID := uuid.New()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	a, ca := h.Subscribe(ctx, jobID)
	b, cb := h.Subscribe(ctx, jobID)
	defer ca()
	defer cb()

	ev := application.JobEvent{JobID: jobID, State: "running", Attempt: 0, UpdatedAt: time.Now()}
	h.Publish(jobID, ev)

	for _, ch := range []<-chan application.JobEvent{a, b} {
		select {
		case got := <-ch:
			if got.State != "running" {
				t.Fatalf("state: %q", got.State)
			}
		case <-time.After(time.Second):
			t.Fatal("subscriber did not receive event")
		}
	}
}

func TestHub_CancelUnregisters(t *testing.T) {
	t.Parallel()
	h := eventbus.NewMemoryHub()
	jobID := uuid.New()

	ctx, cancel := context.WithCancel(context.Background())
	_, c := h.Subscribe(ctx, jobID)
	if got := h.SubscriberCount(jobID); got != 1 {
		t.Fatalf("count after subscribe: %d", got)
	}
	c()
	if got := h.SubscriberCount(jobID); got != 0 {
		t.Fatalf("count after cancel: %d", got)
	}
	cancel()
}

func TestHub_ContextCancelUnregisters(t *testing.T) {
	t.Parallel()
	h := eventbus.NewMemoryHub()
	jobID := uuid.New()

	ctx, cancel := context.WithCancel(context.Background())
	_, _ = h.Subscribe(ctx, jobID)
	cancel()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if h.SubscriberCount(jobID) == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("subscriber not unregistered after ctx cancel: count=%d", h.SubscriberCount(jobID))
}

func TestHub_SlowSubscriberDropsRatherThanBlocks(t *testing.T) {
	t.Parallel()
	h := eventbus.NewMemoryHub()
	jobID := uuid.New()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, c := h.Subscribe(ctx, jobID)
	defer c()

	// Publish way more than the buffer; if Publish blocked, this hangs.
	const N = 1000
	done := make(chan struct{})
	go func() {
		for i := 0; i < N; i++ {
			h.Publish(jobID, application.JobEvent{JobID: jobID, State: "running"})
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Publish blocked on slow subscriber — must drop instead")
	}
	_ = ch // channel intentionally not drained — events past buffer dropped.
}

func TestHub_ConcurrentSubscribePublishCancel(t *testing.T) {
	t.Parallel()
	h := eventbus.NewMemoryHub()
	jobID := uuid.New()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ch, c := h.Subscribe(ctx, jobID)
			// Drain a few events without blocking — buffer absorbs the
			// rest, then drop. Channel is never closed by the hub.
			go func() {
				for j := 0; j < 4; j++ {
					select {
					case <-ch:
					case <-time.After(50 * time.Millisecond):
						return
					}
				}
			}()
			h.Publish(jobID, application.JobEvent{JobID: jobID, State: "running"})
			time.Sleep(time.Millisecond)
			c()
		}()
	}
	wg.Wait()
	// Allow ctx-cancel goroutines to settle.
	time.Sleep(50 * time.Millisecond)
}
