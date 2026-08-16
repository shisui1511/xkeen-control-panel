package handlers

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/shisui1511/xkeen-control-panel/internal/services"
)

// TerminalClientMessage represents an incoming control or data frame from client
type TerminalClientMessage struct {
	Type string `json:"type"` // "stdin" | "resize" | "ping"
	Data string `json:"data,omitempty"`
	Cols int    `json:"cols,omitempty"`
	Rows int    `json:"rows,omitempty"`
}

// TerminalWebSocket handles interactive PTY streaming over WebSocket
func (a *API) TerminalWebSocket(w http.ResponseWriter, r *http.Request) {
	if a.ptySvc == nil {
		http.Error(w, "PTY service unavailable", http.StatusServiceUnavailable)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	// Parse initial dimensions from query parameters
	cols := 80
	rows := 24
	if qCols := r.URL.Query().Get("cols"); qCols != "" {
		if c, err := strconv.Atoi(qCols); err == nil && c > 0 {
			cols = c
		}
	}
	if qRows := r.URL.Query().Get("rows"); qRows != "" {
		if rowVal, err := strconv.Atoi(qRows); err == nil && rowVal > 0 {
			rows = rowVal
		}
	}

	session, err := a.ptySvc.StartSession(cols, rows)
	if err != nil {
		if err == services.ErrMaxSessionsReached {
			_ = conn.WriteJSON(map[string]string{
				"type":    "error",
				"message": "Maximum active terminal sessions (2) reached",
			})
			_ = conn.WriteControl(
				websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "max sessions reached"),
				time.Now().Add(time.Second),
			)
		} else {
			_ = conn.WriteJSON(map[string]string{
				"type":    "error",
				"message": err.Error(),
			})
			_ = conn.WriteControl(
				websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseInternalServerErr, err.Error()),
				time.Now().Add(time.Second),
			)
		}
		return
	}
	defer session.Close()

	conn.SetReadDeadline(time.Now().Add(wsReadDeadline))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(wsReadDeadline))
		return nil
	})

	var writeMu sync.Mutex
	safeWriteMessage := func(msgType int, data []byte) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return conn.WriteMessage(msgType, data)
	}
	safeWriteJSON := func(v interface{}) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return conn.WriteJSON(v)
	}

	stopCh := make(chan struct{})
	var closeOnce sync.Once
	closeAll := func() {
		closeOnce.Do(func() {
			close(stopCh)
			_ = session.Close()
			_ = conn.Close()
		})
	}
	defer closeAll()

	// Ping ticker goroutine
	go func() {
		ticker := time.NewTicker(wsPingInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				writeMu.Lock()
				err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second))
				writeMu.Unlock()
				if err != nil {
					closeAll()
					return
				}
			case <-stopCh:
				return
			case <-session.Done():
				closeAll()
				return
			}
		}
	}()

	// PTY stdout/stderr -> WebSocket
	go func() {
		defer closeAll()
		buf := make([]byte, 4096)
		for {
			select {
			case <-stopCh:
				return
			default:
			}

			n, err := session.Read(buf)
			if n > 0 {
				if err := safeWriteMessage(websocket.TextMessage, buf[:n]); err != nil {
					return
				}
			}
			if err != nil {
				if err == io.EOF {
					_ = safeWriteJSON(map[string]interface{}{
						"type": "exit",
						"code": 0,
					})
				}
				return
			}
		}
	}()

	// WebSocket stdin / resize -> PTY
	for {
		msgType, p, err := conn.ReadMessage()
		if err != nil {
			break
		}

		if msgType == websocket.BinaryMessage {
			if _, err := session.Write(p); err != nil {
				break
			}
			continue
		}

		if msgType == websocket.TextMessage {
			var clientMsg TerminalClientMessage
			if err := json.Unmarshal(p, &clientMsg); err == nil && clientMsg.Type != "" {
				switch clientMsg.Type {
				case "resize":
					if clientMsg.Cols > 0 && clientMsg.Rows > 0 {
						_ = session.Resize(clientMsg.Cols, clientMsg.Rows)
					}
				case "stdin":
					if _, err := session.Write([]byte(clientMsg.Data)); err != nil {
						return
					}
				case "ping":
					writeMu.Lock()
					_ = conn.WriteControl(websocket.PongMessage, nil, time.Now().Add(5*time.Second))
					writeMu.Unlock()
				default:
					if _, err := session.Write(p); err != nil {
						return
					}
				}
			} else {
				// Raw text fallback
				if _, err := session.Write(p); err != nil {
					break
				}
			}
		}
	}
}
