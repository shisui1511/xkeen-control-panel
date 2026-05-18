package handlers

import (
	"bufio"
	"net/http"
	"net/url"
	"os/exec"
	"strings"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true // allow non-browser clients
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

	sources := a.cfg.LogSources
	if len(sources) == 0 {
		sources = []string{a.cfg.LogPath}
	}

	ctx := r.Context()

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
