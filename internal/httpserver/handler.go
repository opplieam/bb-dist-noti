package httpserver

import (
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
)

type handler struct {
}

func newHandler() *handler {
	return &handler{}
}

func (h *handler) SSE(c *gin.Context) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

	for i := 0; i < 10; i++ {
		time.Sleep(1 * time.Second)
		_, _ = fmt.Fprintf(c.Writer, "data: %s\n\n", time.Now())
		c.Writer.Flush()
	}

}
