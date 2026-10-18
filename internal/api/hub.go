package api

import (
	"encoding/json"
	"net/http"
	"sync"
)

// Hub distribui os eventos para os navegadores conectados em /api/events.
// Um navegador lento demais é desconectado; ao reconectar, recebe um snapshot novo.
type Hub struct {
	mu      sync.Mutex
	clients map[*client]struct{}
}

type client struct {
	ch     chan []byte
	closed bool
}

const clientBuffer = 4096

func NewHub() *Hub { return &Hub{clients: map[*client]struct{}{}} }

func frame(v any) []byte {
	b, _ := json.Marshal(v)
	return append(append([]byte("data: "), b...), '\n', '\n')
}

func (h *Hub) add(first ...any) *client {
	h.mu.Lock()
	defer h.mu.Unlock()
	c := &client{ch: make(chan []byte, clientBuffer)}
	for _, v := range first {
		c.ch <- frame(v)
	}
	h.clients[c] = struct{}{}
	return c
}

func (h *Hub) remove(c *client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.drop(c)
}

func (h *Hub) drop(c *client) {
	if !c.closed {
		c.closed = true
		close(c.ch)
	}
	delete(h.clients, c)
}

// Publish serializa na hora (quem chama pode mudar o valor logo depois).
func (h *Hub) Publish(v any) {
	msg := frame(v)
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		select {
		case c.ch <- msg:
		default:
			h.drop(c)
		}
	}
}

// Stream atende um navegador em Server-Sent Events: manda `first` e depois cada Publish.
func (h *Hub) Stream(w http.ResponseWriter, r *http.Request, first ...any) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming não suportado", 500)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(200)
	c := h.add(first...)
	defer h.remove(c)
	for {
		select {
		case msg, ok := <-c.ch:
			if !ok {
				return
			}
			if _, err := w.Write(msg); err != nil {
				return
			}
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

// Close desconecta todos.
func (h *Hub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		h.drop(c)
	}
}
