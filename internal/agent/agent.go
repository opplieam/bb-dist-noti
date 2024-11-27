/*
Package agent provides a high Availability Agent for distributed systems.

The agent manages client connections, handles HTTP requests, and sets up gRPC
and membership services using Raft consensus and Serf-based discovery.
It logs various events and errors during its operation and ensures proper shutdown procedures to close the store gracefully.

Key Components:
1. Client Connection Management: The agent maintains active connections with clients and handles reconnection logic in case of failures.
2. HTTP Request Handling: It processes incoming HTTP requests, routing them to appropriate handlers based on the request path and method.
3. gRPC Service Setup: The agent configures a gRPC server that exposes APIs for distributed system operations.
4. Membership Service: Utilizing Serf, the agent manages node discovery and failure detection in the cluster.
5. Raft Consensus: For maintaining consistent state across nodes, the agent uses the Raft consensus algorithm to handle leader election and log replication.
6. Logging: Comprehensive logging is implemented to track events such as request handling, connection management, and system errors.
7. Graceful Shutdown: The agent ensures a smooth shutdown of services when the application receives an interrupt or termination signal.

This package is designed to be highly available and fault-tolerant, making it suitable for use in critical distributed systems where reliability is paramount.
*/
package agent

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"sync"

	"github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/logging"
	"github.com/hashicorp/raft"
	"github.com/opplieam/bb-dist-noti/internal/clientstate"
	"github.com/opplieam/bb-dist-noti/internal/discovery"
	"github.com/opplieam/bb-dist-noti/internal/grpcserver"
	"github.com/opplieam/bb-dist-noti/internal/httpserver"
	"github.com/opplieam/bb-dist-noti/internal/store"
	"github.com/opplieam/bb-dist-noti/internal/streammanager"
	"github.com/soheilhy/cmux"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

type Agent struct {
	Config Config
	logger *slog.Logger

	mux        cmux.CMux
	store      *store.DistributedStore
	leaderCh   chan bool
	gServer    *grpc.Server
	cState     *clientstate.ClientState
	hServer    *http.Server
	membership *discovery.Membership

	js *streammanager.Manager

	shutdown bool
	//shutdownCh chan struct{}
	shutdownMu sync.Mutex
}

// NewAgent creates and initializes a new instance of the Agent.
// It sets up various components such as logger, client state, HTTP server,
// JetStream, cmux multiplexer, distributed store, gRPC server, and membership.
// The setup functions are executed in sequence, and if any function fails,
// an error is returned. The serve method runs in a separate goroutine to start
// handling incoming connections. The initialized agent instance is returned on success.
func NewAgent(config Config) (*Agent, error) {
	a := &Agent{
		Config:   config,
		leaderCh: make(chan bool, 1),
		//shutdownCh: make(chan struct{}),
	}
	setup := []func() error{
		a.setupLogger,
		a.setupClientState,
		a.setupJetStream,
		a.setupMux,
		a.setupStore,
		a.setupGRPCServer,
		a.setupHTTPServer,
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

// Shutdown gracefully shuts down the agent.
// It ensures that all resources are properly released in a controlled manner,
// including closing HTTP server, JetStream connection, client state, membership,
// distributed store, leader channel, gRPC server, and cmux multiplexer.
func (a *Agent) Shutdown() error {
	a.shutdownMu.Lock()
	defer a.shutdownMu.Unlock()
	if a.shutdown {
		return nil
	}
	a.shutdown = true
	//close(a.shutdownCh)

	shutdown := []func() error{
		func() error {
			ctx, cancel := context.WithTimeout(context.Background(), a.Config.HttpConfig.ShutdownTimeout)
			defer cancel()
			if err := a.hServer.Shutdown(ctx); err != nil {
				_ = a.hServer.Close()
				return fmt.Errorf("http server could not shutdown gratefuly: %w", err)
			}
			return nil
		},
		func() error {
			if a.js == nil {
				a.logger.Debug("not a leader, skip close Jetstream")
				return nil
			}
			return a.js.Close()
		},
		a.cState.Close,
		a.membership.Leave,
		a.store.Close,
		func() error {
			close(a.leaderCh)
			return nil
		},
		func() error {
			a.gServer.GracefulStop()
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

// setupLogger sets up the logger for the agent based on the environment configuration.
// It creates either a JSON handler for production environments or a text handler for other environments.
// The logger is set as the default logger and includes a component tag.
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

// setupClientState initializes the client state for the agent.
// It creates a new instance of ClientState which is used to manage client-connection.
func (a *Agent) setupClientState() error {
	a.cState = clientstate.NewClientState()
	return nil
}

// setupHTTPServer sets up the HTTP server for the agent.
// It configures the HTTP server with necessary parameters from the configuration,
// specifically enabling Server-Sent Events (SSE) for real-time communication.
// The server runs in a separate goroutine and logs its address for reference.
func (a *Agent) setupHTTPServer() error {
	a.Config.HttpConfig.CState = a.cState
	a.Config.HttpConfig.Env = a.Config.Env
	a.Config.HttpConfig.Store = a.store
	a.hServer = httpserver.NewServer(a.Config.HttpConfig)
	a.logger.Info("setup http server", slog.String("address", a.Config.HttpConfig.Addr))
	go func() {
		if err := a.hServer.ListenAndServe(); err != nil {
			_ = a.Shutdown()
		}
	}()
	return nil
}

// setupMux initializes the cmux multiplexer for handling different types of connections.
// It listens on the RPC address specified in the configuration, creates a new multiplexer instance,
// and logs the address for reference.
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

// setupStore initializes the distributed store for the agent.
// It performs the following steps:
//  1. Creates a custom matcher function for the cmux multiplexer to identify Raft RPC connections.
//  2. Configures the stream layer with the appropriate listener and TLS settings.
//  3. Sets up the Raft configuration with the node's RPC address, local ID, bootstrap mode,
//     and a notification channel for leader changes.
//  4. Configures the FSM (Finite State Machine) with a history size limit and client state.
//  5. Logs the setup information for the store.
//  6. Creates a new distributed store instance with the provided configuration.
//
// If any error occurs during these steps, it returns the error.
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
	storeConfig.Raft.NotifyCh = a.leaderCh
	// FSM config
	storeConfig.FSM.Limit = a.Config.HistorySize
	storeConfig.FSM.ClientState = a.cState
	a.logger.Info("setup store", slog.String("Addr", rpcAddr))
	a.store, err = store.NewDistributedStore(a.Config.DataDir, storeConfig)
	if err != nil {
		return err
	}
	// Bootstrap cluster for only first time
	// Servers = 1 only for the first time of running
	//servers, _ := a.store.GetServers()
	//if a.Config.Bootstrap && len(servers) == 1 {
	//	err = a.store.WaitForLeader(3 * time.Second)
	//}
	return nil
}

// setupJetStream sets up JetStream for the agent.
// It performs the following steps:
//  1. Continuously monitors the leadership status through the leader channel.
//  2. When the node becomes a leader, it logs this event and configures JetStream with necessary parameters,
//     including NATS address, stream name, description, subjects, and consumer name.
//  3. Establishes a connection to JetStream using the manager and starts consuming messages.
//  4. If the node loses leadership, it logs this event and closes the JetStream connection.
//
// Only the leader is allowed to establish a NATs connection.
// This function is responsible for subscribing to data from other services using JetStream.
func (a *Agent) setupJetStream() error {
	// Only Leader can establish jet stream connection
	go func() {
		for isLeader := range a.leaderCh {
			if isLeader {
				a.logger.Info("Node become leader", slog.String("leader", a.Config.NodeName))
				cfg := streammanager.Config{
					NatsAddr:    a.Config.NatsAddr,
					StreamName:  "jobs",
					Description: "Consumer for processing category messages with retry and backoff strategy.",
					Subjects: []string{
						"jobs.noti.>",
					},
					ConsumerName: "worker_noti",
				}
				var err error
				a.js, err = streammanager.NewManager(context.Background(), cfg, a.store)
				if err != nil {
					a.logger.Error("create stream manager", slog.String("error", err.Error()))
					return
				}
				err = a.js.ConsumeMessages()
				if err != nil {
					a.logger.Error("consume messages", slog.String("error", err.Error()))
					return
				}

			} else {
				a.logger.Info("Node lost leadership", slog.String("leader", a.Config.NodeName))
				_ = a.js.Close()
			}
		}
	}()
	return nil
}

// setupGRPCServer sets up the internal gRPC server for the agent.
// It configures TLS if enabled, attaches logging interceptors,
// and starts serving on a matched listener from the multiplexer.
// The gRPC server is intended for internal communication within the system.
func (a *Agent) setupGRPCServer() error {
	var opts []grpc.ServerOption
	if a.Config.ServerTLSConfig != nil {
		creds := credentials.NewTLS(a.Config.ServerTLSConfig)
		opts = append(opts, grpc.Creds(creds))
	}

	loggingOpt := []logging.Option{
		logging.WithLogOnEvents(logging.StartCall, logging.FinishCall),
	}
	serverOpt := grpc.ChainUnaryInterceptor(
		logging.UnaryServerInterceptor(grpcserver.InterceptorLogger(a.logger), loggingOpt...),
	)

	opts = append(opts, serverOpt)

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

// setupMembership initializes the membership service for the agent.
// It sets up Serf-based discovery with the necessary configuration,
// including node name, address, tags, start join addresses, and bootstrap mode.
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

// serve starts serving the multiplexer and handles incoming connections.
// It blocks until an error occurs or the agent is shut down.
func (a *Agent) serve() error {
	if err := a.mux.Serve(); err != nil {
		return a.Shutdown()
	}
	return nil
}
