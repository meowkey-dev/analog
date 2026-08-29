// Package sse is the SSE fan-out.
//
// In-process only: one server, one machine. A subscriber gets the backlog from its
// Last-Event-ID first, then live events, so a reconnecting browser never has a gap
// and never sees an event twice.
package sse

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/meowkey-dev/analog/internal/store"
)

const (
	HeartbeatInterval = 15 * time.Second
	// A subscriber this far behind is dropped rather than backpressuring a write.
	queueDepth = 256
	// How much backlog one connection replays before going live.
	backlogLimit = 1000
)

type subscriber chan store.Event

type Broker struct {
	mu   sync.Mutex
	subs map[string]map[subscriber]bool
}

func NewBroker() *Broker {
	return &Broker{subs: map[string]map[subscriber]bool{}}
}

func (b *Broker) Subscribe(spaceID string) subscriber {
	b.mu.Lock()
	defer b.mu.Unlock()
	ch := make(subscriber, queueDepth)
	if b.subs[spaceID] == nil {
		b.subs[spaceID] = map[subscriber]bool{}
	}
	b.subs[spaceID][ch] = true
	return ch
}

func (b *Broker) Unsubscribe(spaceID string, ch subscriber) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.drop(spaceID, ch)
}

// drop removes and closes a subscriber. Callers hold the lock.
func (b *Broker) drop(spaceID string, ch subscriber) {
	if set, ok := b.subs[spaceID]; ok {
		if set[ch] {
			delete(set, ch)
			close(ch)
		}
		if len(set) == 0 {
			delete(b.subs, spaceID)
		}
	}
}

// Publish fans one event out. A full queue means the client has stalled: it is
// dropped so the write that produced this event is never held up, and it replays
// the backlog when it reconnects.
func (b *Broker) Publish(spaceID string, event store.Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subs[spaceID] {
		select {
		case ch <- event:
		default:
			b.drop(spaceID, ch)
		}
	}
}

// Frame renders one SSE message. `event:` is the event type, per openapi
// streamEvents; `id:` is the seq, which is what Last-Event-ID resumes from.
func Frame(event store.Event) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	// Card text is frequently HTML; escaping it to < would be valid JSON but
	// needlessly unreadable on the wire.
	enc.SetEscapeHTML(false)
	if err := enc.Encode(event); err != nil {
		return ""
	}
	data := bytes.TrimRight(buf.Bytes(), "\n")
	return fmt.Sprintf("id: %d\nevent: %s\ndata: %s\n\n", event.Seq, event.Type, data)
}

// Stream serves one subscriber until it disconnects.
func Stream(w http.ResponseWriter, r *http.Request, st *store.Store, broker *Broker,
	spaceID string, since int64) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	write := func(s string) bool {
		if _, err := w.Write([]byte(s)); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}

	if !write(": connected\n\n") {
		return
	}

	// Subscribe before reading the backlog: an event committed between the two is
	// then queued rather than lost, and `last` suppresses the duplicate.
	ch := broker.Subscribe(spaceID)
	defer broker.Unsubscribe(spaceID, ch)

	last := since
	backlog, err := st.Events(spaceID, since, backlogLimit)
	if err != nil {
		return
	}
	for _, event := range backlog {
		if !write(Frame(event)) {
			return
		}
		last = event.Seq
	}

	ticker := time.NewTicker(HeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			if !write(": keepalive\n\n") {
				return
			}
		case event, open := <-ch:
			if !open {
				return // dropped for being too far behind; reconnect and replay
			}
			if event.Seq <= last {
				continue // already sent as backlog
			}
			last = event.Seq
			if !write(Frame(event)) {
				return
			}
		}
	}
}
