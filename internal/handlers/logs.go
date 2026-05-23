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

	// Dynamically append other existing standard log files
	for _, df := range []string{
		"/opt/var/log/xray/access.log",
		"/opt/var/log/xray/error.log",
		"/opt/var/log/xkeen-detached.log",
	} {
		already := false
		for _, s := range sources {
			if s == df {
				already = true
				break
			}
		}
		if !already {
			if _, err := os.Stat(df); err == nil {
				sources = append(sources, df)
			}
		}
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

	// Validate log sources using pathVal
	var validSources []string
	for _, src := range sources {
		if clean, err := a.pathVal.Validate(src); err == nil {
			validSources = append(validSources, clean)
		}
	}
	if len(validSources) == 0 {
		_ = conn.WriteMessage(websocket.TextMessage, []byte("[system] No valid log sources configured\n"))
		return
	}

	var hasShownWaiting bool

	for {
		var existingSources []string
		for _, src := range validSources {
			if _, err := os.Stat(src); err == nil {
				existingSources = append(existingSources, src)
			}
		}

		if len(existingSources) > 0 {
			hasShownWaiting = false
			var cmd *exec.Cmd
			if len(existingSources) == 1 {
				cmd = exec.CommandContext(ctx, "tail", "-f", existingSources[0])
			} else {
				args := append([]string{"-f"}, existingSources...)
				cmd = exec.CommandContext(ctx, "tail", args...)
			}
			stdout, err := cmd.StdoutPipe()
			if err != nil {
				select {
				case <-ctx.Done():
					return
				case <-time.After(2 * time.Second):
					continue
				}
			}

			if err := cmd.Start(); err != nil {
				select {
				case <-ctx.Done():
					return
				case <-time.After(2 * time.Second):
					continue
				}
			}

			// Read from tail and send to WS
			runTailReader := func() error {
				defer cmd.Process.Kill()
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
					if err := conn.WriteMessage(websocket.TextMessage, []byte(line+"\n")); err != nil {
						return err
					}
				}
				return scanner.Err()
			}

			err = runTailReader()
			if err != nil {
				if ctx.Err() != nil {
					return
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
