package service_test

import (
	"errors"
	"testing"

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
