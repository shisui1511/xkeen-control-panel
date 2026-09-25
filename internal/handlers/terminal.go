package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"
	"sync"
	"syscall"
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
	if cols <= 0 {
		cols = 80
	} else if cols > 1000 {
		cols = 1000
	}

	if qRows := r.URL.Query().Get("rows"); qRows != "" {
		if rowVal, err := strconv.Atoi(qRows); err == nil && rowVal > 0 {
			rows = rowVal
		}
	}
	if rows <= 0 {
		rows = 24
	} else if rows > 500 {
		rows = 500
	}

	var session *services.PTYSession
	if r.URL.Query().Get("mode") == "xkeen-install" {
		var release func()
		session, release, err = a.startXKeenInstall(r, conn, cols, rows)
		if release != nil {
			defer release()
		}
		if err != nil && session == nil && release == nil {
			// Ошибка уже показана в терминале
			return
		}
	} else {
		session, err = a.ptySvc.StartSession(cols, rows)
	}
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
				// Соединение закроет читатель вывода: сначала он дочитает
				// остаток и отправит кадр exit
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
				if err := safeWriteMessage(websocket.BinaryMessage, buf[:n]); err != nil {
					return
				}
			}
			if err != nil {
				var pathErr *os.PathError
				if errors.Is(err, io.EOF) || errors.Is(err, os.ErrClosed) || errors.Is(err, syscall.EIO) || (errors.As(err, &pathErr) && errors.Is(pathErr.Err, syscall.EIO)) {
					_ = safeWriteJSON(map[string]interface{}{
						"type": "exit",
						"code": session.ExitCode(2 * time.Second),
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
					cols := clientMsg.Cols
					rows := clientMsg.Rows
					if cols > 0 && rows > 0 {
						if cols > 1000 {
							cols = 1000
						}
						if rows > 500 {
							rows = 500
						}
						_ = session.Resize(cols, rows)
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

// startXKeenInstall скачивает официальный установщик XKeen и запускает его в
// PTY вместо оболочки. Ход подготовки выводится в терминал. Если session и
// release равны nil при ошибке — сообщение уже отправлено клиенту.
func (a *API) startXKeenInstall(r *http.Request, conn *websocket.Conn, cols, rows int) (*services.PTYSession, func(), error) {
	notify := func(color, msg string) {
		_ = conn.WriteMessage(websocket.BinaryMessage, []byte("\x1b["+color+"m"+msg+"\x1b[0m\r\n"))
	}
	fail := func(msg string) (*services.PTYSession, func(), error) {
		notify("31", msg)
		_ = conn.WriteJSON(map[string]interface{}{"type": "exit", "code": 1})
		return nil, nil, errors.New(msg)
	}

	if a.xkeenInstaller == nil {
		return fail("XKeen installer is unavailable")
	}
	if !a.xkeenInstaller.Available() {
		return fail(services.ErrXKeenNoEntware.Error())
	}
	channel := r.URL.Query().Get("channel")
	if _, ok := services.XKeenChannels[channel]; !ok {
		return fail("Unknown XKeen channel: " + channel)
	}
	release, err := a.xkeenInstaller.Acquire()
	if err != nil {
		return fail(err.Error())
	}

	notify("36", "Downloading XKeen installer...")
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	scriptPath, source, err := a.xkeenInstaller.Download(ctx)
	cancel()
	if err != nil {
		release()
		return fail(err.Error())
	}
	notify("32", "Installer downloaded: "+source)

	argv, err := a.xkeenInstaller.Command(scriptPath, channel)
	if err != nil {
		_ = os.Remove(scriptPath)
		release()
		return fail(err.Error())
	}
	session, err := a.ptySvc.StartCommand(cols, rows, argv)
	if err != nil {
		_ = os.Remove(scriptPath)
		// Ошибку старта (в т.ч. лимит сессий) обработает общий код
		return nil, release, err
	}
	return session, release, nil
}
