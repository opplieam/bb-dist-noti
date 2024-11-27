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
	hServer    *http.Server
	membership *discovery.Membership

	js *streammanager.Manager

	shutdown bool
	//shutdownCh chan struct{}
	shutdownMu sync.Mutex
}

func NewAgent(config Config) (*Agent, error) {
	a := &Agent{
		Config:   config,
		leaderCh: make(chan bool, 1),
		//shutdownCh: make(chan struct{}),
	}
	setup := []func() error{
		a.setupLogger,
		a.setupHTTPServer,
		a.setupJetStream,
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
		a.js.Close,
		a.membership.Leave,
		func() error {
			err := a.store.Close()
			close(a.leaderCh)
			return err
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

func (a *Agent) setupHTTPServer() error {
	a.hServer = httpserver.NewServer(a.Config.HttpConfig)
	a.logger.Info("setup http server", slog.String("address", a.Config.HttpConfig.Addr))
	go func() {
		if err := a.hServer.ListenAndServe(); err != nil {
			_ = a.Shutdown()
		}
	}()
	return nil
}

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

func (a *Agent) serve() error {
	if err := a.mux.Serve(); err != nil {
		return a.Shutdown()
	}
	return nil
}
