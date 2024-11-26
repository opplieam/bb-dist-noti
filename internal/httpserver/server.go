package httpserver

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	sloggin "github.com/samber/slog-gin"
)

type Config struct {
	Addr            string
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

	h := newHandler()
	r.GET("/category", h.SSE)

	srv := &http.Server{
		Addr:         cfg.Addr,
		WriteTimeout: cfg.WriteTimeout,
		ReadTimeout:  cfg.ReadTimeout,
		IdleTimeout:  cfg.IdleTimeout,
		Handler:      r.Handler(),
	}
	return srv
}
