package logstream

import (
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type Event struct {
	Type      string         `json:"type"`
	Timestamp string         `json:"timestamp"`
	Level     string         `json:"level"`
	Source    string         `json:"source"`
	Event     string         `json:"event"`
	Message   string         `json:"message"`
	Raw       string         `json:"raw,omitempty"`
	Details   map[string]any `json:"details,omitempty"`
}

type Hub struct {
	mu      sync.Mutex
	logs    []Event
	clients map[*websocket.Conn]chan Event
}

func NewHub() *Hub { return &Hub{clients: make(map[*websocket.Conn]chan Event)} }

func (h *Hub) Publish(level, event, message string, details map[string]any) {
	h.publish(Event{Type: "log", Timestamp: time.Now().Format(time.RFC3339), Level: level, Source: "听雨框架", Event: event, Message: message, Details: details})
}

func (h *Hub) PublishRaw(event, raw string) {
	h.publish(Event{Type: "log", Timestamp: time.Now().Format(time.RFC3339), Level: "warn", Source: "听雨框架", Event: event, Message: "收到 QQ 离线事件", Raw: raw})
}

func (h *Hub) publish(e Event) {
	h.mu.Lock()
	h.logs = append(h.logs, e)
	if len(h.logs) > 200 {
		h.logs = h.logs[len(h.logs)-200:]
	}
	for _, queue := range h.clients {
		select {
		case queue <- e:
		default:
		}
	}
	h.mu.Unlock()
}

func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	queue := make(chan Event, 32)
	h.mu.Lock()
	logs := append([]Event(nil), h.logs...)
	h.clients[conn] = queue
	h.mu.Unlock()
	defer func() { h.mu.Lock(); delete(h.clients, conn); h.mu.Unlock(); _ = conn.Close() }()
	for _, event := range logs {
		if err := conn.WriteJSON(event); err != nil {
			return
		}
	}
	for {
		if err := conn.WriteJSON(<-queue); err != nil {
			return
		}
	}
}
