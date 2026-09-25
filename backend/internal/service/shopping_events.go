// Package service holds business rules. It calls the store and never speaks HTTP.
package service

import (
	"errors"
	"sync"

	"github.com/google/uuid"
)

// Shopping-list event types, sent as the SSE "event:" field and the "type"
// member of each event's JSON data.
const (
	ListEventItemChanged = "item_changed"
	ListEventItemDeleted = "item_deleted"
	ListEventListChanged = "list_changed"
	ListEventListDeleted = "list_deleted"
)

// listSubscriptionBuffer is how many undelivered events one subscriber may
// have queued before it is considered too slow and disconnected (see
// ListEventHub.Publish).
const listSubscriptionBuffer = 32

// ErrEventStreamsClosed means the hub has been closed for shutdown and
// accepts no new subscribers.
var ErrEventStreamsClosed = errors.New("event streams are closed")

// ListEvent is one change to a shopping list, as delivered to its event
// streams. ItemID and Version are set for item_changed and item_deleted
// (Version is the item's version after the change, or for item_deleted the
// version it had when it was removed), and nil for list_changed and
// list_deleted.
type ListEvent struct {
	Type    string
	ListID  uuid.UUID
	ItemID  *uuid.UUID
	Version *int
}

// ListEventHub fans shopping-list events out to the event streams open in
// this API process, keyed by list id. It is in memory: this API is one Go
// process backed only by Postgres, with no message broker, so a second API
// replica would not see this replica's events (clients still converge,
// because they refetch on reconnect and on foreground; see spec §4.3).
//
// Every subscriber has a bounded buffer. Publish never blocks: a subscriber
// whose buffer is full is disconnected (its channel is closed) rather than
// having events silently dropped. Item versions are per item, so a client
// can only notice a dropped event if a later event arrives for that same
// item; closing the stream instead makes the client reconnect and refetch,
// which always converges.
type ListEventHub struct {
	mu     sync.Mutex
	subs   map[uuid.UUID]map[*ListSubscription]struct{}
	closed bool
}

// NewListEventHub returns an empty hub.
func NewListEventHub() *ListEventHub {
	return &ListEventHub{subs: make(map[uuid.UUID]map[*ListSubscription]struct{})}
}

// ListSubscription is one open event stream for one list. It remembers who
// watches it and who owns the list, so the hub can end the streams of a
// partner whose access ended (see CloseAccess).
type ListSubscription struct {
	hub     *ListEventHub
	listID  uuid.UUID
	watcher uuid.UUID
	owner   uuid.UUID
	events  chan ListEvent
}

// Events delivers the list's events in publish order. It is closed when the
// list is deleted (after the list_deleted event), when the subscriber falls
// too far behind, when the hub shuts down, or after Close.
func (s *ListSubscription) Events() <-chan ListEvent { return s.events }

// Close unsubscribes. It is safe to call more than once, and after the hub
// has already closed the channel.
func (s *ListSubscription) Close() {
	s.hub.mu.Lock()
	defer s.hub.mu.Unlock()
	s.hub.removeLocked(s)
}

// Subscribe opens a subscription to listID's events for watcherID, the user
// reading the stream, on a list owned by ownerID. It does not check who may
// see the list: callers go through ShoppingLists.Subscribe, which does.
func (h *ListEventHub) Subscribe(listID, watcherID, ownerID uuid.UUID) (*ListSubscription, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil, ErrEventStreamsClosed
	}
	sub := &ListSubscription{
		hub: h, listID: listID, watcher: watcherID, owner: ownerID,
		events: make(chan ListEvent, listSubscriptionBuffer),
	}
	if h.subs[listID] == nil {
		h.subs[listID] = make(map[*ListSubscription]struct{})
	}
	h.subs[listID][sub] = struct{}{}
	return sub, nil
}

// Publish delivers ev to every subscriber of ev.ListID without blocking. A
// subscriber whose buffer is full is disconnected. A list_deleted event is
// the last one: every subscriber of that list is disconnected right after it
// is queued (a closed channel still yields the values buffered before it).
func (h *ListEventHub) Publish(ev ListEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for sub := range h.subs[ev.ListID] {
		select {
		case sub.events <- ev:
		default:
			h.removeLocked(sub)
		}
	}
	if ev.Type == ListEventListDeleted {
		for sub := range h.subs[ev.ListID] {
			h.removeLocked(sub)
		}
	}
}

// Subscribers reports how many subscriptions listID currently has. Used by
// tests to wait until a stream is listening before publishing.
func (h *ListEventHub) Subscribers(listID uuid.UUID) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs[listID])
}

// Close disconnects every subscriber and refuses new ones. cmd/api registers
// it with http.Server.RegisterOnShutdown: Shutdown does not cancel request
// contexts, so without this an open event stream would never finish and
// every shutdown with a connected client would wait out the full timeout.
func (h *ListEventHub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	for _, set := range h.subs {
		for sub := range set {
			h.removeLocked(sub)
		}
	}
}

// CloseAccess disconnects every stream where userA watches a list userB owns,
// or userB watches a list userA owns. Partners.Unlink calls it after the
// partnership row is deleted, so a partner's open streams end instead of
// outliving their access. Clients reconnect, refetch and get 404.
func (h *ListEventHub) CloseAccess(userA, userB uuid.UUID) {
	h.closeMatching(func(s *ListSubscription) bool {
		return (s.watcher == userA && s.owner == userB) || (s.watcher == userB && s.owner == userA)
	})
}

// CloseListForNonOwners disconnects every stream of listID whose watcher is
// not the list's owner. Called when the owner turns sharing off.
func (h *ListEventHub) CloseListForNonOwners(listID uuid.UUID) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for sub := range h.subs[listID] {
		if sub.watcher != sub.owner {
			h.removeLocked(sub)
		}
	}
}

// CloseUser disconnects every stream that touches userID: one they watch, or
// one on a list they own. Called on account deletion, which removes the
// user's lists with a raw DELETE and so publishes nothing.
func (h *ListEventHub) CloseUser(userID uuid.UUID) {
	h.closeMatching(func(s *ListSubscription) bool {
		return s.watcher == userID || s.owner == userID
	})
}

func (h *ListEventHub) closeMatching(match func(*ListSubscription) bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, set := range h.subs {
		for sub := range set {
			if match(sub) {
				h.removeLocked(sub)
			}
		}
	}
}

// removeLocked unsubscribes sub and closes its channel, at most once. The
// caller holds h.mu; every send and close happens under it, so a send can
// never hit a closed channel.
func (h *ListEventHub) removeLocked(sub *ListSubscription) {
	set := h.subs[sub.listID]
	if _, ok := set[sub]; !ok {
		return
	}
	delete(set, sub)
	if len(set) == 0 {
		delete(h.subs, sub.listID)
	}
	close(sub.events)
}
