package stream

import (
	"sync"
)

type Hub struct {
	mu      sync.Mutex
	clients map[chan []byte]struct{}
	last    []byte
	hasLast bool
}

// NewHub 创建消息广播中心
func NewHub() *Hub {
	return &Hub{clients: map[chan []byte]struct{}{}}
}

func (h *Hub) Add(ch chan []byte) {
	h.mu.Lock()
	h.clients[ch] = struct{}{}
	if h.hasLast {
		select {
		case ch <- h.last:
		default:
		}
	}
	h.mu.Unlock()
}

func (h *Hub) Remove(ch chan []byte) {
	h.mu.Lock()
	delete(h.clients, ch)
	h.mu.Unlock()
}

func (h *Hub) Broadcast(msg []byte) {
	h.mu.Lock()
	h.last = msg
	h.hasLast = true
	defer h.mu.Unlock()
	for ch := range h.clients {
		select {
		case ch <- msg:
		default:
			// Drop if the client is too slow.
		}
	}
}

func (h *Hub) Last() ([]byte, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.hasLast {
		return nil, false
	}
	// Return a copy to avoid callers mutating internal state.
	b := make([]byte, len(h.last))
	copy(b, h.last)
	return b, true
}

func (h *Hub) ClientCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.clients)
}
