package handlers

import (
	"bufio"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/gorilla/websocket"
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

	sources := a.cfg.LogSources
	if len(sources) == 0 {
		sources = []string{a.cfg.LogPath}
	}

	ctx := r.Context()

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

	var cmd *exec.Cmd
	if len(sources) == 1 {
		cmd = exec.CommandContext(ctx, "tail", "-f", sources[0])
	} else {
		args := append([]string{"-f"}, sources...)
		cmd = exec.CommandContext(ctx, "tail", args...)
	}
	stdout, _ := cmd.StdoutPipe()

	if err := cmd.Start(); err != nil {
		conn.WriteMessage(websocket.TextMessage, []byte("Failed to start log stream\n"))
		return
	}
	defer cmd.Process.Kill()

	scanner := bufio.NewScanner(stdout)
	currentSource := ""
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
		}

		line := scanner.Text()
		if strings.HasPrefix(line, "==> ") && strings.HasSuffix(line, " <==") {
			currentSource = line[4 : len(line)-4]
			continue
		}
		if currentSource != "" && len(sources) > 1 {
			line = "[" + currentSource + "] " + line
		}
		if err := conn.WriteMessage(websocket.TextMessage, []byte(line+"\n")); err != nil {
			break
		}
	}
}

func (a *API) LogsDownload(w http.ResponseWriter, r *http.Request) {
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

	w.Header().Set("Content-Disposition", "attachment; filename="+filepath.Base(cleanPath))
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	http.ServeFile(w, r, cleanPath)
}
