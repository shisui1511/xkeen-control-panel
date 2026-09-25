package handlers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/shisui1511/xkeen-control-panel/internal/services"
)

const (
	wsPingInterval = 30 * time.Second
	wsReadDeadline = 60 * time.Second
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return false // reject requests without Origin header
		}
		u, err := url.Parse(origin)
		if err != nil {
			return false
		}
		return u.Host == r.Host
	},
}

func (a *API) LogsWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	// Set initial read deadline; pong handler will extend it on every pong.
	conn.SetReadDeadline(time.Now().Add(wsReadDeadline))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(wsReadDeadline))
		return nil
	})

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// Ping goroutine: sends a ping every wsPingInterval and closes conn on failure.
	stopPing := make(chan struct{})
	go func() {
		ticker := time.NewTicker(wsPingInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second)); err != nil {
					return
				}
			case <-stopPing:
				return
			case <-ctx.Done():
				return
			}
		}
	}()
	defer close(stopPing)

	if a.logDispatcher != nil {
		ch := a.logDispatcher.Subscribe()
		defer a.logDispatcher.Unsubscribe(ch)

		// Send initial history
		initialHistory := a.logDispatcher.GetHistory("all", "", 200)
		if len(initialHistory) > 0 {
			if data, err := json.Marshal(initialHistory); err == nil {
				if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
					return
				}
			}
		}

		// Read pump to handle client pongs/closes and guarantee prompt unsubscribe on disconnect
		go func() {
			for {
				if _, _, err := conn.ReadMessage(); err != nil {
					_ = conn.Close()
					cancel()
					break
				}
			}
		}()

		for {
			select {
			case <-ctx.Done():
				return
			case batch, ok := <-ch:
				if !ok {
					return
				}
				if len(batch) > 0 {
					data, err := json.Marshal(batch)
					if err != nil {
						continue
					}
					if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
						return
					}
				}
			}
		}
	}

	// Чтение нужно, чтобы заметить уход клиента (и обработать pong): иначе
	// tail работает, пока в лог не придёт строка и запись не упадёт
	go func() {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				cancel()
				return
			}
		}
	}()

	sources := a.cfg.LogSources
	if len(sources) == 0 {
		sources = []string{a.cfg.LogPath}
	}

	// Стандартные логи ядер добавляются всегда: файл (и даже его каталог)
	// может появиться уже после подключения — например, mihomo.log после
	// переключения Xray → Mihomo
	for _, df := range []string{
		"/opt/var/log/xray/access.log",
		"/opt/var/log/xray/error.log",
		"/opt/var/log/xkeen-detached.log",
		"/opt/var/log/mihomo.log",
	} {
		if !slices.Contains(sources, df) {
			sources = append(sources, df)
		}
	}

	// existingLogSources — прошедшие PathValidator и существующие сейчас
	// источники. Пересчитывается на каждом круге: Validate отклоняет файл,
	// пока нет его каталога
	existingLogSources := func() []string {
		var existing []string
		for _, src := range sources {
			clean, err := a.pathVal.Validate(src)
			if err != nil {
				continue
			}
			if _, err := os.Stat(clean); err == nil && !slices.Contains(existing, clean) {
				existing = append(existing, clean)
			}
		}
		return existing
	}

	rescanInterval := a.logsRescanInterval
	if rescanInterval <= 0 {
		rescanInterval = defaultLogsRescanInterval
	}

	var hasShownWaiting bool

	for {
		existingSources := existingLogSources()

		if len(existingSources) > 0 {
			hasShownWaiting = false
			// tailCtx отменяется, когда появился новый файл лога: tail
			// перезапускается уже с ним
			tailCtx, stopTail := context.WithCancel(ctx)
			args := append([]string{"-f"}, existingSources...)
			cmd := exec.CommandContext(tailCtx, "tail", args...)
			go func() {
				ticker := time.NewTicker(rescanInterval)
				defer ticker.Stop()
				for {
					select {
					case <-tailCtx.Done():
						return
					case <-ticker.C:
						if len(existingLogSources()) > len(existingSources) {
							stopTail()
							return
						}
					}
				}
			}()
			stdout, err := cmd.StdoutPipe()
			if err != nil {
				stopTail()
				select {
				case <-ctx.Done():
					return
				case <-time.After(2 * time.Second):
					continue
				}
			}

			if err := cmd.Start(); err != nil {
				stopTail()
				select {
				case <-ctx.Done():
					return
				case <-time.After(2 * time.Second):
					continue
				}
			}

			// Read from tail and send to WS
			runTailReader := func() error {
				// Wait после Kill: иначе каждый переподключённый tail остаётся зомби
				defer func() {
					_ = cmd.Process.Kill()
					_ = cmd.Wait()
				}()
				scanner := bufio.NewScanner(stdout)
				currentSource := ""
				for scanner.Scan() {
					select {
					case <-ctx.Done():
						return ctx.Err()
					default:
					}

					line := scanner.Text()
					if strings.HasPrefix(line, "==> ") && strings.HasSuffix(line, " <==") {
						currentSource = line[4 : len(line)-4]
						continue
					}
					if currentSource != "" && len(existingSources) > 1 {
						line = "[" + filepath.Base(currentSource) + "] " + line
					}
					redacted := services.RedactSensitiveText(line)
					entry := services.LogEntry{
						Timestamp: time.Now().Format("15:04:05"),
						Source:    filepath.Base(currentSource),
						Level:     "info",
						Message:   redacted,
					}
					data, _ := json.Marshal([]services.LogEntry{entry})
					if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
						return err
					}
				}
				return scanner.Err()
			}

			err = runTailReader()
			restarted := tailCtx.Err() != nil
			stopTail()
			if ctx.Err() != nil {
				return
			}
			if restarted {
				// Появился новый файл: сразу перезапустить tail с ним
				continue
			}
			if err != nil {
				select {
				case <-ctx.Done():
					return
				case <-time.After(2 * time.Second):
				}
			}
		} else {
			if !hasShownWaiting {
				if err := conn.WriteMessage(websocket.TextMessage, []byte("[system] Waiting for log files to be created...\n")); err != nil {
					return
				}
				hasShownWaiting = true
			}
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Second):
		}
	}
}

// defaultLogsRescanInterval — как часто запасной поток логов проверяет,
// не появились ли новые файлы логов.
const defaultLogsRescanInterval = 2 * time.Second

// LogsHistory returns recent entries from in-memory ring buffers.
func (a *API) LogsHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}

	source := r.URL.Query().Get("source")
	level := r.URL.Query().Get("level")
	limit := 300

	if a.logDispatcher == nil {
		a.jsonResponse(w, map[string]interface{}{"entries": []services.LogEntry{}})
		return
	}

	entries := a.logDispatcher.GetHistory(source, level, limit)
	a.jsonResponse(w, map[string]interface{}{"entries": entries})
}

// LogsFlashHealth returns flash memory health and storage metrics.
func (a *API) LogsFlashHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}

	if a.logDispatcher == nil {
		a.jsonResponse(w, services.FlashHealthInfo{})
		return
	}

	health := a.logDispatcher.GetFlashHealth()
	a.jsonResponse(w, health)
}

// LogsSetLevel updates runtime log-level dynamically.
func (a *API) LogsSetLevel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Source string `json:"source"`
		Level  string `json:"level"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		a.errorResponse(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Source == "mihomo" {
		if !validMihomoLogLevels[req.Level] {
			a.errorResponse(w, "Invalid log level", http.StatusBadRequest)
			return
		}
		if err := a.setMihomoLogLevel(req.Level); err != nil {
			a.errorResponse(w, err.Error(), http.StatusBadGateway)
			return
		}
		a.jsonResponse(w, map[string]bool{"success": true})
		return
	}

	if a.logDispatcher != nil {
		if err := a.logDispatcher.SetLogLevel(req.Source, req.Level); err != nil {
			a.errorResponse(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	a.jsonResponse(w, map[string]bool{"success": true})
}

// LogsClear clears in-memory ring buffers and optionally truncates log files.
func (a *API) LogsClear(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Source string `json:"source"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	if a.logDispatcher != nil {
		a.logDispatcher.ClearBuffers(req.Source)
	}

	a.jsonResponse(w, map[string]bool{"success": true})
}

func (a *API) LogsDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}
	if a.cfg.LogPath == "" {
		a.errorResponse(w, "Log path is not configured", http.StatusBadRequest)
		return
	}

	cleanPath, err := a.pathVal.Validate(a.cfg.LogPath)
	if err != nil {
		a.errorResponse(w, err.Error(), http.StatusForbidden)
		return
	}

	if _, err := os.Stat(cleanPath); os.IsNotExist(err) {
		a.errorResponse(w, "Log file does not exist", http.StatusNotFound)
		return
	}

	f, err := os.Open(cleanPath)
	if err != nil {
		a.errorResponse(w, "Failed to read log file", http.StatusInternalServerError)
		return
	}
	defer f.Close()

	w.Header().Set("Content-Disposition", "attachment; filename="+filepath.Base(cleanPath))
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	redactedReader := services.NewRedactionReader(f)
	_, _ = io.Copy(w, redactedReader)
}

var validMihomoLogLevels = map[string]bool{
	"silent": true, "error": true, "warning": true, "info": true, "debug": true,
}

// setMihomoLogLevel changes Mihomo's runtime log level through the actual
// external-controller (TCP or unix socket) with the configured secret.
func (a *API) setMihomoLogLevel(level string) error {
	if a.cfg == nil && a.mihomoSvc == nil {
		return errors.New("mihomo controller is not configured")
	}
	payload, err := json.Marshal(map[string]string{"log-level": level})
	if err != nil {
		return err
	}
	client, baseURL := a.getMihomoHTTPClientAndBaseURL()
	req, err := http.NewRequest(http.MethodPatch, baseURL+"/configs", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if secret := a.ResolveMihomoSecret(); secret != "" {
		req.Header.Set("Authorization", "Bearer "+secret)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("mihomo API returned status %d", resp.StatusCode)
	}
	return nil
}
