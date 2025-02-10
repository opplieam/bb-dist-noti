// Package discovery provides membership management functionality using Serf and a custom Handler interface.
//
// This package enables discovery of nodes in a distributed system by utilizing HashiCorp's Serf library.
// It maintains an active set of members based on join/leave events received from Serf.
// The core component is the Membership struct, which encapsulates configuration options,
// event handling for Serf, and methods to interact with a custom Handler interface.
package discovery

import (
	"log/slog"
	"net"

	"github.com/hashicorp/serf/serf"
)

// Handler defines the interface for handling join and leave events.
type Handler interface {
	Join(name, addr string) error
	Leave(name string) error
	IsLeader() bool
}

// Config represents the configuration options for Membership.
type Config struct {
	NodeName      string
	SerfAddr      string
	Tags          map[string]string
	StartJoinAddr []string
	Bootstrap     bool
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
		logger:  slog.Default().With("component", "membership"),
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

	// Allow Bootstrap node to define StartJoinAddr,
	// Because Bootstrap node can be down and need to rejoin the cluster
	_, err = m.serf.Join(m.Config.StartJoinAddr, true)
	if err != nil && !m.Config.Bootstrap {
		return err
	}
	return nil
}

// Close gracefully shuts down the node from the cluster.
func (m *Membership) Close() error {
	// defer close(m.events)
	var err error
	err = m.serf.Leave()
	if err != nil {
		err = m.serf.Shutdown()
	}
	return err
}

// eventHandler processes Serf events, reacting to membership changes.
// As leader, it calls handler.Join/Leave for member events.
// Unhandled events are logged for debugging.
// Runs continuously until the event channel is closed.
//
//nolint:gocognit  // This function is complex due to handling multiple event types, but it's maintainable.
func (m *Membership) eventHandler() {
	for e := range m.events {
		switch e.EventType() {
		case serf.EventMemberJoin:
			if !m.handler.IsLeader() {
				continue
			}
			// If 10 nodes join around the same time, Serf will send 1 join event with 10 members
			for _, member := range e.(serf.MemberEvent).Members {
				if err := m.handler.Join(member.Name, member.Tags["rpc_addr"]); err != nil {
					m.logger.Error("failed to join member", "member", member, "err", err)
				}
			}
		case serf.EventMemberLeave, serf.EventMemberFailed:
			if !m.handler.IsLeader() {
				continue
			}
			for _, member := range e.(serf.MemberEvent).Members {
				if err := m.handler.Leave(member.Name); err != nil {
					m.logger.Error("failed to leave member", "member", member, "err", err)
				}
			}
		case serf.EventMemberUpdate, serf.EventMemberReap, serf.EventUser, serf.EventQuery:
			m.logger.Debug("unhandled event", "event", e.EventType().String())
		}
	}
}

// Members returns the current list of members in the cluster.
// Used in test.
func (m *Membership) Members() []serf.Member {
	return m.serf.Members()
}
