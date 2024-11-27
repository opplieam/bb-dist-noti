package httpserver

import (
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	notiApi "github.com/opplieam/bb-dist-noti/protogen/notification_v1"
)

type ServerRetriever interface {
	GetServers() ([]*notiApi.Server, error)
}

type probeHandler struct {
	serverRetriever ServerRetriever
	env             string
}

func newProbeHandler(env string, sr ServerRetriever) *probeHandler {
	return &probeHandler{env: env, serverRetriever: sr}
}

func (h *probeHandler) Liveness(c *gin.Context) {
	host, err := os.Hostname()
	if err != nil {
		host = "unavailable"
	}
	c.JSON(http.StatusOK, gin.H{
		"hostname": host,
		"build":    h.env,
		"status":   "up",
	})
}

type ReadinessOutput struct {
	ServerID   string
	RPCAddress string
	IsLeader   bool
}

func (h *probeHandler) Readiness(c *gin.Context) {
	servers, err := h.serverRetriever.GetServers()
	if err != nil {
		c.Status(http.StatusServiceUnavailable)
		return
	}
	var output []ReadinessOutput
	for _, server := range servers {
		out := ReadinessOutput{ServerID: server.Id, RPCAddress: server.RpcAddr, IsLeader: server.IsLeader}
		output = append(output, out)
	}
	c.JSON(http.StatusOK, output)
}
