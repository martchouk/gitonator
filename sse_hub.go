package main

import (
	"encoding/json"
	"sync"
)

// SSEEvent is a single server-sent event sent to connected dashboard clients.
type SSEEvent struct {
	Type string      `json:"type"`
	Data interface{} `json:"data"`
}

// SSEHub fans out SSE events to all registered browser clients.
type SSEHub struct {
	mu      sync.RWMutex
	clients map[chan []byte]struct{}
}

func newSSEHub() *SSEHub {
	return &SSEHub{
		clients: make(map[chan []byte]struct{}),
	}
}

// Register adds a new client channel. The caller owns the channel and must
// call Unregister when the client disconnects.
func (h *SSEHub) Register(ch chan []byte) {
	h.mu.Lock()
	h.clients[ch] = struct{}{}
	h.mu.Unlock()
}

// Unregister removes a client channel and closes it.
func (h *SSEHub) Unregister(ch chan []byte) {
	h.mu.Lock()
	delete(h.clients, ch)
	h.mu.Unlock()
	close(ch)
}

// Broadcast sends an event to all registered clients. Sends are non-blocking;
// clients that are full are skipped (their next SSE reconnect will catch up).
func (h *SSEHub) Broadcast(evt SSEEvent) {
	b, err := json.Marshal(evt)
	if err != nil {
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for ch := range h.clients {
		select {
		case ch <- b:
		default:
		}
	}
}

// ClientCount returns the number of currently connected SSE clients.
func (h *SSEHub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}
