package ws

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

	"plum/internal/db"
)

var clientSequence atomic.Uint64

type Client struct {
	id      string
	user    *db.User
	hub     *Hub
	conn    *websocket.Conn
	send    chan []byte
	onClose func(*Client)
	onText  func(*Client, []byte)
}

type ServeOptions struct {
	CheckOrigin func(*http.Request) bool
	User        *db.User
	OnClose     func(*Client)
	OnText      func(*Client, []byte)
}

func (c *Client) ID() string {
	return c.id
}

func (c *Client) User() *db.User {
	return c.user
}

func (c *Client) Send(msg []byte) bool {
	select {
	case c.send <- msg:
		return true
	default:
		return false
	}
}

func ServeWS(hub *Hub, w http.ResponseWriter, r *http.Request, options ServeOptions) error {
	upgrader := websocket.Upgrader{
		CheckOrigin: options.CheckOrigin,
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("upgrade ws: %v", err)
		return err
	}
	client := &Client{
		id:      newClientID(),
		user:    options.User,
		hub:     hub,
		conn:    conn,
		send:    make(chan []byte, 16),
		onClose: options.OnClose,
		onText:  options.OnText,
	}
	hub.Register(client)

	// Send welcome message
	welcome, _ := json.Marshal(map[string]string{
		"type":    "welcome",
		"message": "connected to plum",
	})
	client.send <- welcome

	go client.writeLoop()
	go client.readLoop()
	return nil
}

func (c *Client) readLoop() {
	defer func() {
		if c.onClose != nil {
			c.onClose(c)
		}
		c.hub.Unregister(c)
		c.conn.Close()
	}()

	c.conn.SetReadLimit(1024)
	c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, msg, err := c.conn.ReadMessage()
		if err != nil {
			break
		}
		if c.onText != nil {
			c.onText(c, msg)
		}
		// Very simple protocol: only support {"action":"ping"} for now.
		var payload map[string]string
		if err := json.Unmarshal(msg, &payload); err == nil {
			if payload["action"] == "ping" {
				pong, _ := json.Marshal(map[string]string{
					"type": "pong",
				})
				c.send <- pong
			}
		}
	}
}

func newClientID() string {
	return time.Now().UTC().Format("20060102150405.000000000") + "-" + strconv.FormatUint(clientSequence.Add(1), 10)
}

func (c *Client) writeLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case msg, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
