// Package discovery provides membership management functionality using Serf and a custom Handler interface.
//
// This package enables discovery of nodes in a distributed system by utilizing HashiCorp's Serf library.
// It maintains an active set of members based on join/leave events received from Serf.
// The core component is the Membership struct, which encapsulates configuration options,
// event handling for Serf, and methods to interact with a custom Handler interface.
package discovery

import (
	"context"
	"errors"
	"log/slog"
	"net"

	"github.com/hashicorp/raft"
	"github.com/hashicorp/serf/serf"
)

// Handler defines the interface for handling join and leave events.
type Handler interface {
	Join(name, addr string) error
	Leave(name string) error
}

// Config represents the configuration options for Membership.
type Config struct {
	NodeName      string
	SerfAddr      string
	Tags          map[string]string
	StartJoinAddr []string
}

// Membership manages membership of nodes using Serf and interacts with a custom Handler.
type Membership struct {
	Config  Config
	handler Handler
	serf    *serf.Serf      // The Serf instance used for discovering nodes.
	events  chan serf.Event // Channel to receive Serf events.
	logger  *slog.Logger    // Logger for logging various operations and errors.
}

// NewMembership creates a new Membership instance with the given Handler and Config.
func NewMembership(handler Handler, config Config) (*Membership, error) {
	m := &Membership{
		Config:  config,
		handler: handler,
		logger:  slog.With("component", "membership"),
	}
	if err := m.setupSerf(); err != nil {
		return nil, err
	}
	return m, nil
}

// setupSerf initializes and starts the Serf instance. It sets up event handling,
// binds to the specified address, and attempts to join any provided start addresses.
func (m *Membership) setupSerf() error {
	addr, err := net.ResolveTCPAddr("tcp", m.Config.SerfAddr)
	if err != nil {
		return err
	}
	config := serf.DefaultConfig()
	config.Init()
	config.MemberlistConfig.BindAddr = addr.IP.String()
	config.MemberlistConfig.BindPort = addr.Port

	m.events = make(chan serf.Event)
	config.EventCh = m.events
	config.Tags = m.Config.Tags
	config.NodeName = m.Config.NodeName

	m.serf, err = serf.Create(config)
	if err != nil {
		return err
	}
	go m.eventHandler()
	if m.Config.StartJoinAddr != nil {
		_, err = m.serf.Join(m.Config.StartJoinAddr, true)
		if err != nil {
			return err
		}
	}
	return nil
}

// eventHandler continuously listens for Serf events and processes them based on their type.
// It handles join/leave events by calling handleJoin/handleLeave methods respectively.
func (m *Membership) eventHandler() {
	for e := range m.events {
		switch e.EventType() {
		case serf.EventMemberJoin:
			// If 10 nodes join around the same time, Serf will send 1 join event with 10 members
			for _, member := range e.(serf.MemberEvent).Members {
				// Serf sent event to all nodes. including the node that joined if left the cluster
				// Prevent act on itself
				if m.isLocal(member) {
					continue
				}
				if err := m.handler.Join(member.Name, member.Tags["rpc_addr"]); err != nil {
					m.logError(err, "failed to join", member)
				}
			}
		case serf.EventMemberLeave, serf.EventMemberFailed:
			for _, member := range e.(serf.MemberEvent).Members {
				if m.isLocal(member) {
					return
				}
				if err := m.handler.Leave(member.Name); err != nil {
					m.logError(err, "failed to leave", member)
				}
			}
		}
	}
}

// isLocal checks if the given member is the local node.
func (m *Membership) isLocal(member serf.Member) bool {
	return m.serf.LocalMember().Name == member.Name
}

// Members returns the current list of members in the cluster.
// Used in test
func (m *Membership) Members() []serf.Member {
	return m.serf.Members()
}

// Leave gracefully shuts down the node from the cluster.
// Used in test
func (m *Membership) Leave() error {
	return m.serf.Leave()
}

// logError logs errors based on their type. Raft will return ErrNotLeader when attempting to
// make changes to the cluster from a non-leader node. If the error is due to being a non-leader,
// it should be expected and not logged
func (m *Membership) logError(err error, msg string, member serf.Member) {
	level := slog.LevelError
	if errors.Is(err, raft.ErrNotLeader) {
		level = slog.LevelDebug
	}
	m.logger.Log(
		context.Background(),
		level,
		msg,
		slog.String("error", err.Error()),
		slog.String("name", member.Name),
		slog.String("rpc_addr", member.Tags["rpc_addr"]),
	)

}
