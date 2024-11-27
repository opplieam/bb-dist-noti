package httpserver

import (
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/opplieam/bb-dist-noti/internal/clientstate"
)

type handler struct {
	clientState *clientstate.ClientState
}

func newHandler(cState *clientstate.ClientState) *handler {
	return &handler{clientState: cState}
}

func (h *handler) SSE(c *gin.Context) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

	// TODO: Add real User ID
	userKey := c.Query("user_id")
	clientCh := h.clientState.AddClient(userKey)
	defer close(clientCh)
	defer h.clientState.RemoveClient(userKey)

	clientClose := c.Writer.CloseNotify()
	// Handshake
	c.Writer.Flush()
loop:
	for {
		select {
		case v := <-clientCh:
			_, _ = fmt.Fprintf(
				c.Writer,
				"data: UserID: %d Category: %s MatchCategory: %s\n\n", v.UserId, v.CategoryFrom, v.CategoryTo,
			)
			c.Writer.Flush()
		case <-clientClose:
			//fmt.Println("Client disconnected")
			break loop
		}
	}
}
