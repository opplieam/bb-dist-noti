/*
Package agent provides the implementation of an Agent that integrates various components for distributed notification system.
The Agent is responsible for setting up and managing multiple services including gRPC server, membership discovery,
distributed store using Raft consensus, and multiplexing network protocols.

Key Components:

- **Logger**: Configured based on the environment (production or development) to log information or debug details respectively.

- **CMux Multiplexer**: Handles multiple network protocols over a single listener, routing connections to appropriate services.

- **Distributed Store**: Implements a distributed append only store using Raft consensus algorithm for fault-tolerant data storage.

- **gRPC Server**: Provides inter-server communication for the notification system, secured with TLS if configured.
- **Membership Service**: Manages node discovery and membership of the cluster using Serf.

The Agent lifecycle includes methods to start serving (`NewAgent`) and gracefully shut down (`Shutdown`). It ensures that all components
are properly initialized and started in separate goroutines to maintain non-blocking operation. During shutdown, it handles each component's
termination sequence to ensure data integrity and prevent any ongoing requests from failing abruptly.
*/
package agent

import (
	"bytes"
	"io"
	"log/slog"
	"net"
	"os"
	"sync"
	"time"

	"github.com/hashicorp/raft"
	"github.com/opplieam/bb-dist-noti/internal/discovery"
	"github.com/opplieam/bb-dist-noti/internal/grpcserver"
	"github.com/opplieam/bb-dist-noti/internal/store"
	"github.com/soheilhy/cmux"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

type Agent struct {
	Config Config
	logger *slog.Logger

	mux        cmux.CMux
	store      *store.DistributedStore
	gServer    *grpc.Server
	membership *discovery.Membership

	shutdown bool
	//shutdownCh chan struct{}
	shutdownMu sync.Mutex
}

// NewAgent creates a new instance of the Agent struct with the provided configuration.
// It initializes various components such as logger, multiplexer, store, gRPC server, and membership,
// and starts serving them in separate goroutines.
func NewAgent(config Config) (*Agent, error) {
	a := &Agent{
		Config: config,
		//shutdownCh: make(chan struct{}),
	}
	setup := []func() error{
		a.setupLogger,
		a.setupMux,
		a.setupStore,
		a.setupGRPCServer,
		a.setupMembership,
	}
	for _, fn := range setup {
		if err := fn(); err != nil {
			return nil, err
		}
	}
	go func() {
		err := a.serve()
		if err != nil {
			a.logger.Error("mux serve error", slog.String("error", err.Error()))
		}
	}()
	return a, nil
}

// Shutdown gracefully shuts down the agent by stopping all components in a specific order.
// It ensures that no new requests are accepted and existing ones are processed before shutting down.
func (a *Agent) Shutdown() error {
	a.shutdownMu.Lock()
	defer a.shutdownMu.Unlock()
	if a.shutdown {
		return nil
	}
	a.shutdown = true
	//close(a.shutdownCh)

	shutdown := []func() error{
		a.membership.Leave,
		func() error {
			a.gServer.GracefulStop()
			return nil
		},
		a.store.Close,
		func() error {
			a.mux.Close()
			return nil
		},
	}
	for _, fn := range shutdown {
		if err := fn(); err != nil {
			a.logger.Error("shutdown error", slog.String("error", err.Error()))
			continue
		}
	}
	return nil
}

// setupLogger initializes the logger instance based on the agent's configuration.
func (a *Agent) setupLogger() error {
	var logger *slog.Logger
	if a.Config.Env == "prod" {
		logger = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			Level:     slog.LevelInfo,
			AddSource: true,
		}))
	} else {
		logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	}
	slog.SetDefault(logger)

	a.logger = slog.With("component", "agent")
	return nil
}

// setupMux initializes the cmux multiplexer which handles multiple network protocols over a single listener.
func (a *Agent) setupMux() error {
	rpcAddr, err := a.Config.RPCAddr()
	a.logger.Info("setup mux", slog.String("Addr", rpcAddr))
	if err != nil {
		return err
	}
	ln, err := net.Listen("tcp", rpcAddr)
	if err != nil {
		return err
	}
	a.mux = cmux.New(ln)
	return nil
}

// setupStore initializes the distributed store using the Raft consensus algorithm.
// It configures the necessary components for communication, including setting up a listener
// specifically for Raft RPCs using cmux to handle multiple network protocols over a single listener.
func (a *Agent) setupStore() error {
	// Create a custom matcher function for the cmux multiplexer to identify Raft RPC connections
	raftLn := a.mux.Match(func(reader io.Reader) bool {
		b := make([]byte, 1)
		if _, err := reader.Read(b); err != nil {
			return false
		}
		return bytes.Compare(b, []byte{byte(store.RaftRPC)}) == 0
	})

	storeConfig := store.Config{}
	// Stream Layer config
	storeConfig.StreamLayer.RaftLn = raftLn
	storeConfig.StreamLayer.ServerTLSConfig = a.Config.ServerTLSConfig
	storeConfig.StreamLayer.PeerTLSConfig = a.Config.PeerTLSConfig
	// Raft config
	rpcAddr, err := a.Config.RPCAddr()
	if err != nil {
		return err
	}
	storeConfig.Raft.Addr = rpcAddr
	storeConfig.Raft.LocalID = raft.ServerID(a.Config.NodeName)
	storeConfig.Raft.Bootstrap = a.Config.Bootstrap

	a.logger.Info("setup store", slog.String("Addr", rpcAddr))
	a.store, err = store.NewDistributedStore(a.Config.DataDir, storeConfig)
	if err != nil {
		return err
	}
	// Bootstrap cluster for only first time
	// Servers = 1 only for the first time of running
	servers, _ := a.store.GetServers()
	if a.Config.Bootstrap && len(servers) == 1 {
		err = a.store.WaitForLeader(3 * time.Second)
	}
	return err
}

// setupGRPCServer initializes and starts the gRPC server.
// This gRPC server is intended to be used between servers for inter-server communication.
func (a *Agent) setupGRPCServer() error {
	var opts []grpc.ServerOption
	if a.Config.ServerTLSConfig != nil {
		creds := credentials.NewTLS(a.Config.ServerTLSConfig)
		opts = append(opts, grpc.Creds(creds))
	}

	gServerConfig := &grpcserver.NotiConfig{ServerRetriever: a.store}
	a.gServer = grpcserver.NewGrpcServer(gServerConfig, opts...)

	grpcLn := a.mux.Match(cmux.Any())
	a.logger.Info("setup grpc", slog.String("Addr", grpcLn.Addr().String()))
	go func() {
		if err := a.gServer.Serve(grpcLn); err != nil {
			//a.logger.Error("gRPC server error", slog.String("error", err.Error()))
			_ = a.Shutdown()
		}
	}()
	return nil
}

// setupMembership initializes the membership service using Serf.
func (a *Agent) setupMembership() error {
	rpcAddr, err := a.Config.RPCAddr()
	if err != nil {
		return err
	}
	a.logger.Info("setup membership", slog.String("Addr", a.Config.SerfAddr))
	a.membership, err = discovery.NewMembership(a.store, discovery.Config{
		NodeName: a.Config.NodeName,
		SerfAddr: a.Config.SerfAddr,
		Tags: map[string]string{
			"rpc_addr": rpcAddr,
		},
		StartJoinAddr: a.Config.StartJoinAddrs,
		Bootstrap:     a.Config.Bootstrap,
	})
	return err
}

// serve starts the cmux multiplexer to handle incoming connections.
func (a *Agent) serve() error {
	if err := a.mux.Serve(); err != nil {
		return a.Shutdown()
	}
	return nil
}
