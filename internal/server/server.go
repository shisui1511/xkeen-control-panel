package server

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/auth"
	"github.com/shisui1511/xkeen-control-panel/internal/cert"
	"github.com/shisui1511/xkeen-control-panel/internal/i18n"
	"github.com/shisui1511/xkeen-control-panel/internal/middleware"
)

// loopbackAllowedPaths — allowlist служебных путей, обслуживаемых на
// 127.0.0.1:<LoopbackPort> (D-14). Всё остальное, включая вход, setup и
// /api/auth/me, получает 403 без обращения к s.mux — список переживает
// появление новых auth-эндпоинтов без новой дыры: путь недоступен с
// loopback, пока явно не добавлен сюда.
var loopbackAllowedPaths = map[string]bool{
	"/api/provider.yaml":         true,
	"/mihomo/provider.yaml":      true,
	"/mihomo/hwid/provider.yaml": true,
	"/api/version":               true,
}

// noCookieWriter удаляет заголовок Set-Cookie перед отправкой ответа —
// защита на случай, если обработчик из allowlist когда-либо начнёт
// выставлять cookie (T-134-35); сегодня ни один из четырёх путей этого не
// делает, но loopback-поверхность не должна зависеть от будущей дисциплины
// авторов обработчиков.
type noCookieWriter struct {
	http.ResponseWriter
}

func (w *noCookieWriter) WriteHeader(statusCode int) {
	w.Header().Del("Set-Cookie")
	w.ResponseWriter.WriteHeader(statusCode)
}

// Write forwards to the underlying ResponseWriter for every allowlisted
// loopback handler, so CodeQL's go/reflected-xss flags it as a generic sink
// reachable from request-derived data (same shape/false-positive class as the
// already-dismissed alert on maxBytesResponseWriter.Write in
// internal/middleware/maxbytes.go). This is a false positive: since Handle()
// only ever registers loopbackAllowedPaths handlers on s.loopbackMux (Version,
// MihomoProviderAdapter, MihomoProviderRedirect), the only bytes that ever
// reach here are a JSON body (json.NewEncoder, auto-escaping), a text/yaml
// upstream-subscription payload (never an error message that echoes the raw
// request), or net/http.Redirect's own html.EscapeString-ed anchor body over
// a url.Values.Encode()-normalized query. auth.SecurityHeaders additionally
// sets X-Content-Type-Options: nosniff on every loopback response, so a
// browser will never interpret this body as HTML/JS regardless of content.
func (w *noCookieWriter) Write(b []byte) (int, error) {
	w.Header().Del("Set-Cookie")
	return w.ResponseWriter.Write(b)
}

type Server struct {
	cfg         *Config
	version     string
	mux         *http.ServeMux
	loopbackMux *http.ServeMux
	authService *auth.AuthService
	mu          sync.RWMutex
	httpSrv     *http.Server
	loopbackSrv *http.Server
}

type Config struct {
	Port               int
	LoopbackPort       int
	XRayConfigDir      string
	XKeenBinary        string
	MihomoConfigDir    string
	MihomoBinary       string
	AllowedRoots       []string
	LogLevel           string
	DataDir            string
	PasswordHash       string
	MaxLoginAttempts   int
	LockoutDuration    time.Duration
	SessionIdleTTL     time.Duration
	SessionAbsoluteTTL time.Duration
	HTTPS              HTTPSConfig
	SavePasswordHash   func(string) error
}

// HTTPSConfig задаёт свой сертификат панели (D-12). Панель всегда работает
// только по HTTPS — переключателя больше нет (см. Server.Start).
type HTTPSConfig struct {
	CertPath string
	KeyPath  string
}

func New(cfg *Config, version string, web fs.FS) (*Server, error) {
	mux := http.NewServeMux()

	// Serve static files with strict cache control for index.html and long-term cache for assets
	fileServer := http.FileServer(http.FS(web))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		// An unknown API route must fail loudly: answering with the SPA page
		// and 200 hid calls to endpoints that do not exist.
		if strings.HasPrefix(path, "/api/") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"success":false,"error":"unknown API endpoint"}`))
			return
		}
		if path == "/" || path == "/index.html" || filepath.Ext(path) == "" {
			w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
			w.Header().Set("Pragma", "no-cache")
			w.Header().Set("Expires", "0")
			if path != "/" && path != "/index.html" && filepath.Ext(path) == "" {
				trimmed := strings.TrimPrefix(path, "/")
				if f, err := web.Open(trimmed); err != nil {
					r.URL.Path = "/"
				} else {
					_ = f.Close()
				}
			}
		} else if len(path) >= 8 && path[:8] == "/assets/" {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		fileServer.ServeHTTP(w, r)
	})

	authService := auth.NewAuthService(auth.Options{
		PasswordHash:     cfg.PasswordHash,
		MaxLoginAttempts: cfg.MaxLoginAttempts,
		LockoutDuration:  cfg.LockoutDuration,
		IdleTTL:          cfg.SessionIdleTTL,
		AbsoluteTTL:      cfg.SessionAbsoluteTTL,
		DataDir:          cfg.DataDir,
		OnPasswordSet:    cfg.SavePasswordHash,
	})

	return &Server{
		cfg:         cfg,
		version:     version,
		mux:         mux,
		loopbackMux: http.NewServeMux(),
		authService: authService,
	}, nil
}

// Handle регистрирует обработчик в основном mux и, если путь входит в
// loopbackAllowedPaths (D-14), дополнительно — в отдельном loopbackMux.
// BuildLoopbackHandler диспетчеризует именно через loopbackMux, а не через
// общий mux: статически (а не только по рантайм-проверке карты) достижимы
// только явно разрешённые обработчики — остальная поверхность приложения
// (включая обработчики с чувствительными к XSS-скептике данными в других
// частях мукса) недостижима из loopback-листенера даже теоретически, что
// также закрывает false-positive CodeQL go/reflected-xss от общего мукса.
func (s *Server) Handle(pattern string, handler http.HandlerFunc) {
	s.mux.HandleFunc(pattern, handler)
	if loopbackAllowedPaths[pattern] {
		s.loopbackMux.HandleFunc(pattern, handler)
	}
}

func (s *Server) HandleProtected(pattern string, handler http.HandlerFunc) {
	s.mux.HandleFunc(pattern, s.authService.RequireAuth(handler))
}

func (s *Server) GetVersion() string {
	return s.version
}

func (s *Server) GetAuthService() *auth.AuthService {
	return s.authService
}

// BuildHandler constructs and returns the HTTP handler with the complete middleware chain.
func (s *Server) BuildHandler() http.Handler {
	var handler http.Handler = s.mux
	handler = i18n.Middleware(handler)
	handler = auth.SecurityHeaders(handler)
	handler = middleware.Recovery(handler)
	handler = middleware.MaxBytes(handler)
	handler = middleware.Logging(handler)
	return handler
}

// BuildLoopbackHandler constructs the HTTP handler for the 127.0.0.1
// loopback listener (D-14, T-134-34). Unlike BuildHandler, this handler only
// forwards requests whose cleaned path is in loopbackAllowedPaths to
// s.loopbackMux — a separate, minimal ServeMux that only ever has the
// allowlisted patterns registered on it (see Handle) — everything else (in
// particular login, setup and /api/auth/me) gets a 403 JSON response without
// ever reaching a mux, so no cookie-issuing or auth-checking code path is
// reachable from an unauthenticated local process, and no unrelated handler
// on the public mux is even statically reachable from this listener. Allowed
// responses are wrapped in noCookieWriter as defense in depth against a
// future allowlisted handler issuing a cookie.
func (s *Server) BuildLoopbackHandler() http.Handler {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cleanPath := path.Clean(r.URL.Path)
		if !loopbackAllowedPaths[cleanPath] {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"error":   "not available on loopback",
			})
			return
		}
		s.loopbackMux.ServeHTTP(&noCookieWriter{ResponseWriter: w}, r)
	})

	var handler http.Handler = inner
	handler = i18n.Middleware(handler)
	handler = auth.SecurityHeaders(handler)
	handler = middleware.Recovery(handler)
	handler = middleware.MaxBytes(handler)
	handler = middleware.Logging(handler)
	return handler
}

func (s *Server) Start() error {
	handler := s.BuildHandler()

	addr := fmt.Sprintf(":%d", s.cfg.Port)
	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       120 * time.Second,
		ErrorLog:          newServerErrorLog(),
	}

	s.mu.Lock()
	s.httpSrv = httpSrv
	s.mu.Unlock()

	// D-12: панель отвечает только по HTTPS — переключателя https.enabled
	// больше нет (config.Load принудительно ставит его в true, 134-06).
	certPath := s.cfg.HTTPS.CertPath
	keyPath := s.cfg.HTTPS.KeyPath
	if certPath == "" {
		certPath = filepath.Join(s.cfg.DataDir, "ssl", "cert.pem")
	}
	if keyPath == "" {
		keyPath = filepath.Join(s.cfg.DataDir, "ssl", "key.pem")
	}

	if _, err := os.Stat(certPath); os.IsNotExist(err) {
		log.Printf("Generating self-signed certificate: %s", certPath)
		if err := cert.GenerateSelfSigned(certPath, keyPath, nil); err != nil {
			return fmt.Errorf("failed to generate certificate: %w", err)
		}
	}

	tlsConfig, err := cert.LoadOrGenerate(certPath, keyPath, nil)
	if err != nil {
		return fmt.Errorf("failed to load certificate: %w", err)
	}

	raw, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}

	// D-13: тот же порт распознаёт обычный HTTP по первому байту и отвечает
	// 308 на https, не пропуская соединение в tls.Listener/http.Server.
	listener := tls.NewListener(newSniffListener(raw, sniffPeekTimeout), tlsConfig)
	defer listener.Close()

	// Start HTTP loopback server on localhost (127.0.0.1) only. D-14: the
	// loopback listener uses its own allowlist-only handler, NOT the public
	// handler — login/setup/me must not be reachable over plain HTTP.
	loopbackAddr := fmt.Sprintf("127.0.0.1:%d", s.cfg.LoopbackPort)
	loopbackSrv := &http.Server{
		Addr:              loopbackAddr,
		Handler:           s.BuildLoopbackHandler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	s.mu.Lock()
	s.loopbackSrv = loopbackSrv
	s.mu.Unlock()

	go func() {
		log.Printf("Listening HTTP loopback on %s", loopbackAddr)
		if err := loopbackSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("HTTP loopback server error: %v", err)
		}
	}()

	log.Printf("Listening HTTPS on port %d", s.cfg.Port)
	return httpSrv.Serve(listener)
}

// Shutdown gracefully stops the HTTP server, waiting up to ctx deadline for
// active connections to finish.
func (s *Server) Shutdown(ctx context.Context) error {
	s.mu.RLock()
	httpSrv := s.httpSrv
	loopbackSrv := s.loopbackSrv
	s.mu.RUnlock()

	var errs []error
	if httpSrv != nil {
		if err := httpSrv.Shutdown(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	if loopbackSrv != nil {
		if err := loopbackSrv.Shutdown(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("shutdown errors: %v", errs)
	}
	return nil
}
