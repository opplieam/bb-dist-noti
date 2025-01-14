package httpserver

import (
	"crypto/rand"
	"encoding/base64"
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
	userKey, _ := generateUniqueID()
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
				"data: UserID: %d Category: %s MatchCategory: %s\n\n", v.GetUserId(), v.GetCategoryFrom(), v.GetCategoryTo(),
			)
			c.Writer.Flush()
		case <-clientClose:
			// fmt.Println("Client disconnected")
			break loop
		}
	}
}

// generateUniqueID generates a short, URL-safe unique ID.
func generateUniqueID() (string, error) {
	// Generate 5 random bytes
	//nolint:mnd // This is temporary
	bytes := make([]byte, 5)
	_, err := rand.Read(bytes)
	if err != nil {
		return "", fmt.Errorf("failed to generate random bytes: %w", err)
	}

	// Encode to URL-safe Base64 (removes padding "=")
	id := base64.RawURLEncoding.EncodeToString(bytes)
	return id, nil
}
