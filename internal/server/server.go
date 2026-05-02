package server

import (
	"fmt"
	"io/fs"
	"log"
	"net/http"

	"github.com/user/xkeen-control-panel/internal/auth"
)

type Server struct {
	cfg         *Config
	version     string
	mux         *http.ServeMux
	authService *auth.AuthService
}

type Config struct {
	Port            int
	XRayConfigDir   string
	XKeenBinary     string
	MihomoConfigDir string
	MihomoBinary    string
	AllowedRoots    []string
	LogLevel        string
	DataDir         string
	PasswordHash    string
}

func New(cfg *Config, version string, web fs.FS) (*Server, error) {
	mux := http.NewServeMux()
	
	// Serve static files
	mux.Handle("/", http.FileServer(http.FS(web)))

	authService := auth.NewAuthService(cfg.PasswordHash)

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
	log.Printf("Listening on port %d", s.cfg.Port)
	
	// Wrap mux with security headers
	handler := auth.SecurityHeaders(s.mux)
	
	return http.ListenAndServe(fmt.Sprintf(":%d", s.cfg.Port), handler)
}
