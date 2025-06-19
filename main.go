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
	Cohort string // Added cohort support
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
		register:   make(chan *Client, 100),    // Buffered channel
		unregister: make(chan *Client, 100),    // Buffered channel
		broadcast:  make(chan Message, 1000),   // Larger buffer for messages
	}
}

func (h *Hub) run() {
	log.Printf("Hub started and running...")
	for {
		select {
		case client := <-h.register:
			log.Printf("Processing registration for client: %s (%s) for cohort %s", client.ID, client.Type, client.Cohort)
			
			h.mu.Lock()
			// Check if client already exists and close old connection safely
			if existingClient, exists := h.clients[client.ID]; exists {
				log.Printf("Client %s already exists, closing old connection", client.ID)
				// Close connection but don't close channel yet - let unregister handle it
				existingClient.Conn.Close()
			}
			h.clients[client.ID] = client
			h.mu.Unlock()
			
			log.Printf("Client %s (%s) successfully registered to cohort %s", client.ID, client.Type, client.Cohort)
			
			// Send current streamers list to new viewer (filtered by cohort)
			if client.Type == "viewer" {
				log.Printf("Sending streamers list to new viewer %s", client.ID)
				go h.sendStreamersList(client) // Use goroutine to prevent blocking
			} else if client.Type == "streamer" {
				// When a streamer connects, broadcast updated list to all viewers in the cohort
				log.Printf("New streamer %s connected, broadcasting to cohort %s", client.ID, client.Cohort)
				go h.broadcastStreamersListToCohort(client.Cohort) // Use goroutine to prevent blocking
			}

		case client := <-h.unregister:
			log.Printf("Processing unregistration for client: %s", client.ID)
			h.mu.Lock()
			if existingClient, ok := h.clients[client.ID]; ok {
				// Only close if it's the same client instance
				if existingClient == client {
					delete(h.clients, client.ID)
					// Safely close the channel
					select {
					case <-client.Send:
						// Channel already closed
					default:
						close(client.Send)
					}
					log.Printf("Client %s successfully unregistered from cohort %s", client.ID, client.Cohort)
					
					// Notify viewers in the same cohort about updated streamers list
					if client.Type == "streamer" {
						go h.broadcastStreamersListToCohort(client.Cohort) // Use goroutine to prevent blocking
					}
				} else {
					log.Printf("Client %s unregistration ignored - newer instance exists", client.ID)
				}
			} else {
				log.Printf("Client %s not found during unregistration", client.ID)
			}
			h.mu.Unlock()

		case message := <-h.broadcast:
			log.Printf("Processing broadcast message: %s from %s to %s", message.Type, message.From, message.To)
			h.mu.RLock()
			if message.To != "" {
				// Direct message to specific client
				if client, ok := h.clients[message.To]; ok {
					select {
					case client.Send <- message:
						log.Printf("Message sent to %s", message.To)
					default:
						log.Printf("Failed to send message to %s, removing client", message.To)
						close(client.Send)
						delete(h.clients, client.ID)
					}
				} else {
					log.Printf("Target client %s not found", message.To)
				}
			} else {
				// Broadcast to all clients (this shouldn't happen with cohort system)
				log.Printf("Broadcasting to all clients (unexpected)")
				for id, client := range h.clients {
					select {
					case client.Send <- message:
					default:
						log.Printf("Failed to broadcast to %s, removing client", id)
						close(client.Send)
						delete(h.clients, id)
					}
				}
			}
			h.mu.RUnlock()
		}
	}
}

// Send streamers list filtered by cohort
func (h *Hub) sendStreamersList(viewer *Client) {
	h.mu.RLock()
	streamers := []string{}
	for id, client := range h.clients {
		if client.Type == "streamer" && client.Cohort == viewer.Cohort {
			streamers = append(streamers, id)
		}
	}
	h.mu.RUnlock()

	log.Printf("Sending streamers list to viewer %s in cohort %s: %v", viewer.ID, viewer.Cohort, streamers)

	message := Message{
		Type: "streamers-list",
		Data: streamers,
	}
	
	select {
	case viewer.Send <- message:
		log.Printf("Successfully sent streamers list to viewer %s", viewer.ID)
	default:
		log.Printf("Failed to send streamers list to viewer %s - channel full", viewer.ID)
	}
}

// Broadcast updated streamers list to all viewers in a cohort
func (h *Hub) broadcastStreamersListToCohort(cohort string) {
	log.Printf("Broadcasting streamers list update to cohort: %s", cohort)
	
	h.mu.RLock()
	streamers := []string{}
	viewers := []*Client{}
	
	for id, client := range h.clients {
		if client.Cohort == cohort {
			if client.Type == "streamer" {
				streamers = append(streamers, id)
			} else if client.Type == "viewer" {
				viewers = append(viewers, client)
			}
		}
	}
	h.mu.RUnlock()

	log.Printf("Found %d streamers and %d viewers in cohort %s", len(streamers), len(viewers), cohort)
	log.Printf("Streamers in cohort %s: %v", cohort, streamers)

	message := Message{
		Type: "streamers-list",
		Data: streamers,
	}
	
	// Send to all viewers in the cohort
	for _, viewer := range viewers {
		select {
		case viewer.Send <- message:
			log.Printf("Sent streamers list to viewer %s", viewer.ID)
		default:
			log.Printf("Failed to send streamers list to viewer %s - channel full", viewer.ID)
		}
	}
}

func (h *Hub) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	log.Printf("WebSocket connection attempt from %s", r.RemoteAddr)
	
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		http.Error(w, "Could not upgrade connection", http.StatusBadRequest)
		return
	}

	clientID := r.URL.Query().Get("id")
	clientType := r.URL.Query().Get("type")
	cohort := r.URL.Query().Get("cohort")
	
	log.Printf("WebSocket params - ID: %s, Type: %s, Cohort: %s", clientID, clientType, cohort)
	
	// Default cohort if none specified
	if cohort == "" {
		cohort = "global"
	}
	
	if clientID == "" || (clientType != "streamer" && clientType != "viewer") {
		log.Printf("Invalid WebSocket parameters - ID: %s, Type: %s", clientID, clientType)
		conn.Close()
		return
	}

	client := &Client{
		ID:     clientID,
		Conn:   conn,
		Type:   clientType,
		Cohort: cohort,
		Send:   make(chan Message, 256),
	}

	log.Printf("Created client %s (%s) for cohort %s", client.ID, client.Type, client.Cohort)

	// Use non-blocking send to prevent deadlock
	select {
	case h.register <- client:
		log.Printf("Successfully queued registration for client %s", client.ID)
	default:
		log.Printf("ERROR: Registration channel full for client %s", client.ID)
		conn.Close()
		return
	}

	go client.writePump()
	go client.readPump(h)
}

func (c *Client) readPump(hub *Hub) {
	defer func() {
		log.Printf("ReadPump ending for client %s", c.ID)
		hub.unregister <- c
		c.Conn.Close()
	}()

	for {
		var message Message
		err := c.Conn.ReadJSON(&message)
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket error for client %s: %v", c.ID, err)
			} else {
				log.Printf("Client %s disconnected normally", c.ID)
			}
			break
		}

		log.Printf("Received message from client %s: %s", c.ID, message.Type)
		message.From = c.ID
		
		// Use non-blocking send to prevent deadlock
		select {
		case hub.broadcast <- message:
		default:
			log.Printf("Warning: broadcast channel full, dropping message from %s", c.ID)
		}
	}
}

func (c *Client) writePump() {
	defer func() {
		log.Printf("WritePump ending for client %s", c.ID)
		c.Conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.Send:
			if !ok {
				log.Printf("Send channel closed for client %s", c.ID)
				c.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			if err := c.Conn.WriteJSON(message); err != nil {
				log.Printf("WriteJSON error for client %s: %v", c.ID, err)
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