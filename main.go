package main

import (
	"log"
	"net/http"
	"sync"
	"github.com/gorilla/websocket"
	"github.com/rs/cors"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

type Message struct {
	Type string      `json:"type"`
	Data interface{} `json:"data"`
	From string      `json:"from,omitempty"`
	To   string      `json:"to,omitempty"`
}

type Client struct {
	ID     string
	Conn   *websocket.Conn
	Type   string // "streamer" or "viewer"
	Send   chan Message
	mu     sync.Mutex
}

type Hub struct {
	clients    map[string]*Client
	register   chan *Client
	unregister chan *Client
	broadcast  chan Message
	mu         sync.RWMutex
}

func newHub() *Hub {
	return &Hub{
		clients:    make(map[string]*Client),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		broadcast:  make(chan Message),
	}
}

func (h *Hub) run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client.ID] = client
			h.mu.Unlock()
			log.Printf("Client %s (%s) connected", client.ID, client.Type)
			
			// Send current streamers list to new viewer
			if client.Type == "viewer" {
				h.sendStreamersList(client)
			}

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client.ID]; ok {
				delete(h.clients, client.ID)
				close(client.Send)
				log.Printf("Client %s disconnected", client.ID)
			}
			h.mu.Unlock()

		case message := <-h.broadcast:
			h.mu.RLock()
			if message.To != "" {
				// Direct message to specific client
				if client, ok := h.clients[message.To]; ok {
					select {
					case client.Send <- message:
					default:
						close(client.Send)
						delete(h.clients, client.ID)
					}
				}
			} else {
				// Broadcast to all clients
				for _, client := range h.clients {
					select {
					case client.Send <- message:
					default:
						close(client.Send)
						delete(h.clients, client.ID)
					}
				}
			}
			h.mu.RUnlock()
		}
	}
}

func (h *Hub) sendStreamersList(viewer *Client) {
	h.mu.RLock()
	streamers := []string{}
	for id, client := range h.clients {
		if client.Type == "streamer" {
			streamers = append(streamers, id)
		}
	}
	h.mu.RUnlock()

	message := Message{
		Type: "streamers-list",
		Data: streamers,
	}
	
	select {
	case viewer.Send <- message:
	default:
		log.Printf("Failed to send streamers list to viewer %s", viewer.ID)
	}
}

func (h *Hub) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}

	clientID := r.URL.Query().Get("id")
	clientType := r.URL.Query().Get("type")
	
	if clientID == "" || (clientType != "streamer" && clientType != "viewer") {
		conn.Close()
		return
	}

	client := &Client{
		ID:   clientID,
		Conn: conn,
		Type: clientType,
		Send: make(chan Message, 256),
	}

	h.register <- client

	go client.writePump()
	go client.readPump(h)
}

func (c *Client) readPump(hub *Hub) {
	defer func() {
		hub.unregister <- c
		c.Conn.Close()
	}()

	for {
		var message Message
		err := c.Conn.ReadJSON(&message)
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket error: %v", err)
			}
			break
		}

		message.From = c.ID
		hub.broadcast <- message
	}
}

func (c *Client) writePump() {
	defer c.Conn.Close()

	for {
		select {
		case message, ok := <-c.Send:
			if !ok {
				c.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			if err := c.Conn.WriteJSON(message); err != nil {
				log.Printf("WriteJSON error: %v", err)
				return
			}
		}
	}
}

func main() {
	hub := newHub()
	go hub.run()

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", hub.handleWebSocket)
	
	// Serve static files
	mux.Handle("/", http.FileServer(http.Dir("./public/")))

	c := cors.New(cors.Options{
		AllowedOrigins: []string{"*"},
		AllowedMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders: []string{"*"},
	})

	handler := c.Handler(mux)

	log.Println("Server starting on :8080")
	log.Fatal(http.ListenAndServe(":8080", handler))
}
