package store

import (
	"crypto/tls"
	"net"
	"time"

	"github.com/hashicorp/raft"
	"github.com/opplieam/bb-dist-noti/internal/clientstate"
)

type Config struct {
	Raft struct {
		raft.Config
		Addr      string
		Bootstrap bool
	}
	FSM struct {
		Limit       int
		ClientState *clientstate.ClientState
	}
	StreamLayer struct {
		RaftLn          net.Listener
		ServerTLSConfig *tls.Config
		PeerTLSConfig   *tls.Config
	}
	TransportLayer struct {
		MaxPool int
		Timeout time.Duration
	}
}
