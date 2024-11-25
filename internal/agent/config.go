package agent

import (
	"crypto/tls"
	"fmt"
	"net"
)

type Config struct {
	ServerTLSConfig *tls.Config
	PeerTLSConfig   *tls.Config
	DataDir         string
	SerfAddr        string
	RPCPort         int
	NodeName        string
	StartJoinAddrs  []string
	Bootstrap       bool
	Env             string
}

func (c Config) RPCAddr() (string, error) {
	host, _, err := net.SplitHostPort(c.SerfAddr)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s:%d", host, c.RPCPort), nil
}
