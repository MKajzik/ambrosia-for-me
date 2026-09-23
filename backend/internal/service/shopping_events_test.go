package service_test

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/InzKazik/mealplanner/backend/internal/service"
)

func itemEvent(listID uuid.UUID, version int) service.ListEvent {
	itemID := uuid.New()
	return service.ListEvent{Type: service.ListEventItemChanged, ListID: listID, ItemID: &itemID, Version: &version}
}

func TestListEventHubDeliversOnlyToSubscribersOfThatList(t *testing.T) {
	hub := service.NewListEventHub()
	listA, listB := uuid.New(), uuid.New()
	subA, err := hub.Subscribe(listA)
	if err != nil {
		t.Fatalf("Subscribe A: %v", err)
	}
	defer subA.Close()
	subB, err := hub.Subscribe(listB)
	if err != nil {
		t.Fatalf("Subscribe B: %v", err)
	}
	defer subB.Close()

	hub.Publish(itemEvent(listA, 2))

	select {
	case ev := <-subA.Events():
		if ev.ListID != listA || *ev.Version != 2 {
			t.Errorf("A got %+v, want list A at version 2", ev)
		}
	default:
		t.Fatal("A received nothing")
	}
	select {
	case ev := <-subB.Events():
		t.Errorf("B received %+v, want nothing (different list)", ev)
	default:
	}
}

func TestListEventHubDisconnectsASubscriberThatFallsBehind(t *testing.T) {
	hub := service.NewListEventHub()
	list := uuid.New()
	slow, err := hub.Subscribe(list)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer slow.Close()

	// 32 fit in the buffer; the 33rd finds it full.
	for v := 1; v <= 33; v++ {
		hub.Publish(itemEvent(list, v))
	}

	received := 0
	for range slow.Events() {
		received++
	}
	if received != 32 {
		t.Errorf("received %d buffered events before the channel closed, want 32", received)
	}
	if n := hub.Subscribers(list); n != 0 {
		t.Errorf("Subscribers after the overflow = %d, want 0 (disconnected, not silently dropping)", n)
	}
}

func TestListEventHubEndsStreamsAfterListDeleted(t *testing.T) {
	hub := service.NewListEventHub()
	list := uuid.New()
	sub, err := hub.Subscribe(list)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer sub.Close()

	hub.Publish(service.ListEvent{Type: service.ListEventListDeleted, ListID: list})

	ev, open := <-sub.Events()
	if !open || ev.Type != service.ListEventListDeleted {
		t.Fatalf("first receive = %+v (open %v), want the list_deleted event", ev, open)
	}
	if _, open := <-sub.Events(); open {
		t.Error("channel still open after list_deleted, want closed")
	}
}

func TestListEventHubCloseEndsEveryStreamAndRefusesNewOnes(t *testing.T) {
	hub := service.NewListEventHub()
	sub, err := hub.Subscribe(uuid.New())
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	hub.Close()

	if _, open := <-sub.Events(); open {
		t.Error("channel still open after Close, want closed")
	}
	sub.Close() // must not panic on an already-closed subscription
	if _, err := hub.Subscribe(uuid.New()); !errors.Is(err, service.ErrEventStreamsClosed) {
		t.Errorf("Subscribe after Close: err = %v, want ErrEventStreamsClosed", err)
	}
}

// TestListEventHubConcurrentPublishSubscribeClose is the one genuinely
// multi-goroutine test in this file: every other test above calls
// Subscribe/Publish/Close sequentially from the test goroutine, which proves
// the sequential logic is self-consistent but never forces two goroutines to
// contend on the same mutex at the same time. This test does: several
// publisher goroutines and several subscriber goroutines all hammer a small,
// shared pool of list ids (so they land on the same map buckets, not
// independent ones) at the same time, including subscribers that Close
// twice, to exercise removeLocked's documented idempotency under real
// contention rather than a single-goroutine call sequence.
//
// The workload is bounded (fixed publish counts, a fixed per-subscriber
// receive budget with a short per-attempt timeout) rather than an unbounded
// loop, so the test is fast and deterministic under -race -count=N.
func TestListEventHubConcurrentPublishSubscribeClose(t *testing.T) {
	const (
		numLists              = 4 // small on purpose: forces real contention on shared map buckets.
		numPublishers         = 8
		numSubscribers        = 16
		publishesPerPublisher = 100
		maxEventsPerSub       = 5
		recvAttemptTimeout    = 50 * time.Millisecond
	)

	hub := service.NewListEventHub()
	lists := make([]uuid.UUID, numLists)
	for i := range lists {
		lists[i] = uuid.New()
	}

	var (
		wg            sync.WaitGroup
		receivedTotal int64
		subFailures   int64
	)

	// Publishers: concurrently Publish against the shared list ids.
	wg.Add(numPublishers)
	for p := 0; p < numPublishers; p++ {
		go func(p int) {
			defer wg.Done()
			for i := 0; i < publishesPerPublisher; i++ {
				hub.Publish(itemEvent(lists[(p+i)%numLists], i+1))
			}
		}(p)
	}

	// Subscribers: concurrently Subscribe to the same shared list ids, drain
	// a bounded number of events, then Close (every other one twice).
	wg.Add(numSubscribers)
	for s := 0; s < numSubscribers; s++ {
		go func(s int) {
			defer wg.Done()
			list := lists[s%numLists]
			sub, err := hub.Subscribe(list)
			if err != nil {
				// The hub is never closed in this test, so this must never happen.
				atomic.AddInt64(&subFailures, 1)
				return
			}
		drain:
			for i := 0; i < maxEventsPerSub; i++ {
				select {
				case _, ok := <-sub.Events():
					if !ok {
						break drain // disconnected (overflow) — a valid outcome, not an error.
					}
					atomic.AddInt64(&receivedTotal, 1)
				case <-time.After(recvAttemptTimeout):
					break drain // nothing arrived in time; move on to closing.
				}
			}
			sub.Close()
			if s%2 == 0 {
				sub.Close() // must not panic: removeLocked is meant to be idempotent.
			}
		}(s)
	}

	wg.Wait()

	if subFailures != 0 {
		t.Errorf("Subscribe failed %d times, want 0 (the hub is never closed in this test)", subFailures)
	}
	if receivedTotal == 0 {
		t.Fatal("no subscriber ever observed an event across the whole run; the hub delivered nothing")
	}
	t.Logf("subscribers observed %d events across %d publishers x %d publishes to %d shared lists",
		receivedTotal, numPublishers, publishesPerPublisher, numLists)

	// Every subscriber closed (directly or via overflow, followed by an
	// explicit Close), so each list must now show zero subscribers.
	for _, list := range lists {
		if n := hub.Subscribers(list); n != 0 {
			t.Errorf("Subscribers(%v) after every subscriber closed = %d, want 0", list, n)
		}
	}
}
