package server

import (
	"crypto/tls"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"

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
}

type Config struct {
	Port             int
	XRayConfigDir    string
	XKeenBinary      string
	MihomoConfigDir  string
	MihomoBinary     string
	AllowedRoots     []string
	LogLevel         string
	DataDir          string
	PasswordHash     string
	SecureCookie     bool
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

	// Serve static files
	mux.Handle("/", http.FileServer(http.FS(web)))

	authService := auth.NewAuthService(cfg.PasswordHash, cfg.SecureCookie, cfg.SavePasswordHash)

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

func (s *Server) Start() error {
	// Wrap mux with middleware chain
	var handler http.Handler = s.mux
	handler = i18n.Middleware(handler)
	handler = auth.SecurityHeaders(handler)
	handler = middleware.Recovery(handler)
	handler = middleware.Logging(handler)

	addr := fmt.Sprintf(":%d", s.cfg.Port)

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

		log.Printf("Listening HTTPS on port %d", s.cfg.Port)
		return http.Serve(listener, handler)
	}

	log.Printf("Listening HTTP on port %d", s.cfg.Port)
	return http.ListenAndServe(addr, handler)
}
