package server

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/auth"
	"github.com/shisui1511/xkeen-control-panel/internal/cert"
	"github.com/shisui1511/xkeen-control-panel/internal/i18n"
	"github.com/shisui1511/xkeen-control-panel/internal/middleware"
)

type Server struct {
	cfg         *Config
	version     string
	mux         *http.ServeMux
	authService *auth.AuthService
	mu          sync.RWMutex
	httpSrv     *http.Server
	loopbackSrv *http.Server
}

type Config struct {
	Port             int
	LoopbackPort     int
	XRayConfigDir    string
	XKeenBinary      string
	MihomoConfigDir  string
	MihomoBinary     string
	AllowedRoots     []string
	LogLevel         string
	DataDir          string
	PasswordHash     string
	SecureCookie     bool
	MaxLoginAttempts int
	LockoutDuration  time.Duration
	HTTPS            HTTPSConfig
	SavePasswordHash func(string) error
}

type HTTPSConfig struct {
	Enabled  bool
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

	authService := auth.NewAuthService(cfg.PasswordHash, cfg.SecureCookie, cfg.MaxLoginAttempts, cfg.LockoutDuration, cfg.SavePasswordHash)

	return &Server{
		cfg:         cfg,
		version:     version,
		mux:         mux,
		authService: authService,
	}, nil
}

func (s *Server) Handle(pattern string, handler http.HandlerFunc) {
	s.mux.HandleFunc(pattern, handler)
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

	if s.cfg.HTTPS.Enabled {
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

		listener, err := tls.Listen("tcp", addr, tlsConfig)
		if err != nil {
			return fmt.Errorf("failed to listen TLS: %w", err)
		}
		defer listener.Close()

		// Start HTTP loopback server on localhost (127.0.0.1) only
		loopbackAddr := fmt.Sprintf("127.0.0.1:%d", s.cfg.LoopbackPort)
		loopbackSrv := &http.Server{
			Addr:              loopbackAddr,
			Handler:           handler,
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

	log.Printf("Listening HTTP on port %d", s.cfg.Port)
	return httpSrv.ListenAndServe()
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
