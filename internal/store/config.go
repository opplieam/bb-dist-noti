package store

import (
	"crypto/tls"
	"net"
	"time"

	"github.com/hashicorp/raft"
)

type Config struct {
	Raft struct {
		raft.Config
		Addr      string
		Bootstrap bool
	}
	FSM struct {
		Limit int
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
