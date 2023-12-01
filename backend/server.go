package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"

	"github.com/gorilla/websocket"
)

var addr = flag.String("addr", "0.0.0.0:8081", "http service address")

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

type client struct {
	conn         *websocket.Conn
	send         chan []byte
	clientType   string // Add a type to differentiate between Arduino and frontend
}

var (
	clients = make(map[*client]bool)
	broadcast = make(chan []byte)
	mu      sync.Mutex
)

func echo(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Print("upgrade:", err)
		return
	}
	defer conn.Close()

	// Determine the client type based on a query parameter or some identifier
	clientType := r.URL.Query().Get("type")

	cl := &client{conn: conn, send: make(chan []byte, 256), clientType: clientType}
	mu.Lock()
	clients[cl] = true
	mu.Unlock()

	go writePump(cl)

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("error: %v", err)
			}
			break
		}
		broadcast <- message
	}

	mu.Lock()
	delete(clients, cl)
	close(cl.send)
	mu.Unlock()
}

func handleMessages() {
	for {
		msg := <-broadcast
		fmt.Printf("Broadcasting message: %s\n", string(msg))
		mu.Lock()
		for cl := range clients {
			// Only send to frontend clients
			if cl.clientType == "frontend" {
				select {
				case cl.send <- msg:
				default:
					// Handle in writePump
				}
			}
		}
		mu.Unlock()
	}
}

func writePump(cl *client) {
	defer cl.conn.Close()
	for {
		msg, ok := <-cl.send
		if !ok {
			cl.conn.WriteMessage(websocket.CloseMessage, []byte{})
			return
		}
		if err := cl.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
			log.Println("write error:", err)
			return
		}
	}
}

func main() {
	flag.Parse()
	log.SetFlags(0)
	http.HandleFunc("/echo", echo)
	go handleMessages()

	fs := http.FileServer(http.Dir("../client"))
	http.Handle("/", fs)

	log.Fatal(http.ListenAndServe(*addr, nil))
}
