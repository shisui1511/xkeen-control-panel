package server

import (
	"embed"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"sync"
	"time"
)

var webFS embed.FS

type Server struct {
	cfg      *Config
	version  string
	mux      *http.ServeMux
	sessions map[string]*Session
	mu       sync.RWMutex
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
}

type Session struct {
	Token     string
	CreatedAt time.Time
}

func New(cfg *Config, version string, web fs.FS) (*Server, error) {
	mux := http.NewServeMux()
	
	// Serve static files
	mux.Handle("/", http.FileServer(http.FS(web)))

	return &Server{
		cfg:      cfg,
		version:  version,
		mux:      mux,
		sessions: make(map[string]*Session),
	}, nil
}

func (s *Server) Handle(pattern string, handler http.HandlerFunc) {
	s.mux.HandleFunc(pattern, handler)
}

func (s *Server) GetVersion() string {
	return s.version
}

func (s *Server) Start() error {
	log.Printf("Listening on port %d", s.cfg.Port)
	return http.ListenAndServe(fmt.Sprintf(":%d", s.cfg.Port), s.mux)
}

func (s *Server) Start() error {
	log.Printf("Listening on port %d", s.cfg.Port)
	return http.ListenAndServe(fmt.Sprintf(":%d", s.cfg.Port), s.mux)
}

