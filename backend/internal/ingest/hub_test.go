package ingest

import (
	"testing"

	"github.com/google/uuid"
)

func hubEvents(project uuid.UUID, n int) []Event {
	out := make([]Event, n)
	for i := range out {
		out[i] = Event{ProjectID: project, EventID: NewEventID()}
	}
	return out
}

func TestHubDeliversOnlyOwnProject(t *testing.T) {
	h := NewHub(10)
	a, b := uuid.New(), uuid.New()
	s, _, _ := h.Subscribe(a, 0)
	defer h.Unsubscribe(s)
	h.Publish(hubEvents(b, 2))
	h.Publish(hubEvents(a, 1))
	select {
	case te := <-s.C:
		if te.Event.ProjectID != a || te.Cursor != 3 {
			t.Fatalf("got cursor %d of project %s", te.Cursor, te.Event.ProjectID)
		}
	default:
		t.Fatal("no event delivered")
	}
	if len(s.C) != 0 {
		t.Fatalf("%d extra events", len(s.C))
	}
}

func TestHubBacklogAndReset(t *testing.T) {
	h := NewHub(4)
	p := uuid.New()
	h.Publish(hubEvents(p, 6)) // cursors 1..6, the ring keeps 3..6

	s, backlog, ok := h.Subscribe(p, 3)
	h.Unsubscribe(s)
	if !ok || len(backlog) != 3 || backlog[0].Cursor != 4 || backlog[2].Cursor != 6 {
		t.Fatalf("after 3: ok=%v backlog=%v", ok, backlog)
	}
	s, backlog, ok = h.Subscribe(p, 2) // cursor 3 is the oldest kept; 2 is still contiguous
	h.Unsubscribe(s)
	if !ok || len(backlog) != 4 {
		t.Fatalf("after 2: ok=%v len=%d", ok, len(backlog))
	}
	s, _, ok = h.Subscribe(p, 1) // cursor 2 is gone
	h.Unsubscribe(s)
	if ok {
		t.Fatal("after 1: want reset")
	}
	s, _, ok = h.Subscribe(p, 99) // from the future, e.g. a cursor of another process
	h.Unsubscribe(s)
	if ok {
		t.Fatal("after 99: want reset")
	}
	s, backlog, ok = h.Subscribe(p, 6)
	h.Unsubscribe(s)
	if !ok || len(backlog) != 0 {
		t.Fatalf("caught up: ok=%v backlog=%v", ok, backlog)
	}
}

func TestHubDropsSlowSubscriber(t *testing.T) {
	h := NewHub(10)
	p := uuid.New()
	s, _, _ := h.Subscribe(p, 0)
	h.Publish(hubEvents(p, cap(s.C)+1))
	n := 0
	for range s.C {
		n++
	}
	if n != cap(s.C) || h.Drops != 1 || h.Subscribers() != 0 {
		t.Fatalf("read %d, drops %d, subscribers %d", n, h.Drops, h.Subscribers())
	}
	h.Unsubscribe(s) // after a drop: must not panic
}
