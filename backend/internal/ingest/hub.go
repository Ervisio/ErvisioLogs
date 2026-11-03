package ingest

import (
	"sync"

	"github.com/google/uuid"
)

// Hub fans stored events out to live-tail subscribers. It keeps the last RingSize events so a
// reconnecting client can resume from its Last-Event-ID. It lives in one process: with several
// API instances, a client sees only the events written by the instance it is connected to.
type Hub struct {
	mu    sync.Mutex
	ring  []TailEvent
	next  uint64 // cursor of the next event; cursors start at 1
	subs  map[*Sub]struct{}
	size  int
	Drops uint64 // subscribers dropped because they fell behind
}

// TailEvent is an event with its hub cursor.
type TailEvent struct {
	Cursor uint64
	Event  Event
}

// Sub is one subscriber. C closes when the hub drops the subscriber for being slow.
type Sub struct {
	C       chan TailEvent
	project uuid.UUID
	closed  bool
}

// NewHub returns a hub that remembers ringSize events.
func NewHub(ringSize int) *Hub {
	if ringSize <= 0 {
		ringSize = 10000
	}
	return &Hub{ring: make([]TailEvent, 0, ringSize), next: 1, subs: map[*Sub]struct{}{}, size: ringSize}
}

// Publish adds events to the ring and sends them to subscribers of the same project.
func (h *Hub) Publish(events []Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, e := range events {
		te := TailEvent{Cursor: h.next, Event: e}
		h.next++
		if len(h.ring) < h.size {
			h.ring = append(h.ring, te)
		} else {
			h.ring[int(te.Cursor-1)%h.size] = te
		}
		for s := range h.subs {
			if s.project != e.ProjectID {
				continue
			}
			select {
			case s.C <- te:
			default:
				h.dropLocked(s)
			}
		}
	}
}

func (h *Hub) dropLocked(s *Sub) {
	if !s.closed {
		s.closed = true
		close(s.C)
		delete(h.subs, s)
		h.Drops++
	}
}

// Subscribe registers a subscriber for project. If after is not zero, the hub first queues the
// ring events of that project with a cursor greater than after. ok is false when after is older
// than the ring, so the client missed events and should reset its view.
func (h *Hub) Subscribe(project uuid.UUID, after uint64) (s *Sub, backlog []TailEvent, ok bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	s = &Sub{C: make(chan TailEvent, 512), project: project}
	h.subs[s] = struct{}{}
	ok = true
	if after == 0 {
		return s, nil, true
	}
	oldest := h.next - uint64(len(h.ring))
	if after+1 < oldest || after >= h.next {
		ok = false
		return s, nil, ok
	}
	for c := after + 1; c < h.next; c++ {
		te := h.ring[int(c-1)%h.size]
		if te.Event.ProjectID == project {
			backlog = append(backlog, te)
		}
	}
	return s, backlog, ok
}

// Unsubscribe removes s. It is safe to call after the hub dropped s.
func (h *Hub) Unsubscribe(s *Sub) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !s.closed {
		s.closed = true
		delete(h.subs, s)
	}
}

// Subscribers returns the number of open subscriptions.
func (h *Hub) Subscribers() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs)
}
