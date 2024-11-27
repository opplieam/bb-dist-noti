package httpserver

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/opplieam/bb-dist-noti/internal/clientstate"
	"github.com/opplieam/bb-dist-noti/internal/store"
	sloggin "github.com/samber/slog-gin"
)

type Config struct {
	Addr            string
	CState          *clientstate.ClientState
	Store           *store.DistributedStore
	Env             string
	WriteTimeout    time.Duration
	ReadTimeout     time.Duration
	IdleTimeout     time.Duration
	ShutdownTimeout time.Duration
}

func NewServer(cfg Config) *http.Server {
	logger := slog.With("component", "httpserver")
	var r *gin.Engine
	r = gin.New()
	r.Use(sloggin.New(logger))
	r.Use(gin.Recovery())

	h := newHandler(cfg.CState)
	p := newProbeHandler(cfg.Env, cfg.Store)
	r.GET("/category", h.SSE)
	r.GET("/liveness", p.Liveness)
	r.GET("/readiness", p.Readiness)

	srv := &http.Server{
		Addr:         cfg.Addr,
		WriteTimeout: cfg.WriteTimeout,
		ReadTimeout:  cfg.ReadTimeout,
		IdleTimeout:  cfg.IdleTimeout,
		Handler:      r.Handler(),
	}
	return srv
}
