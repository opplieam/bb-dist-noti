// Package grpcserver provides a gRPC server implementation for handling internal notifications between servers.
//
// This package is designed for internal use to facilitate communication between
// different servers within a distributed system.
// It defines:
// - ServerRetriever interface: An interface for retrieving server information.
// - NotiConfig struct: A configuration struct holding the server retriever.
// - NewGrpcServer function: Creates and returns a new gRPC server instance with the given configuration and options.
package grpcserver

import (
	"context"

	notiApi "github.com/opplieam/bb-dist-noti/protogen/notification_v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/protobuf/types/known/emptypb"
)

type ServerRetriever interface {
	GetServers() ([]*notiApi.Server, error)
	IsLeader() bool
}

// NotiConfig struct holds the configuration for the notification server.
type NotiConfig struct {
	ServerRetriever ServerRetriever
}

// notiServer struct implements the gRPC server for notifications.
type notiServer struct {
	notiApi.UnimplementedNotificationServer
	*NotiConfig
}

// newNotiServer creates a new instance of notiServer with the given configuration.
func newNotiServer(config *NotiConfig) *notiServer {
	return &notiServer{
		NotiConfig: config,
	}
}

// GetServers implements the NotificationServer interface for getting server information.
//
// This function is designed to provide all servers' information in a Raft cluster,
// including their leader or follower status. It is intended for internal use between servers
// within the same distributed system.
func (n *notiServer) GetServers(_ context.Context, _ *notiApi.GetServersRequest) (*notiApi.GetServersResponse, error) {
	server, err := n.ServerRetriever.GetServers()
	if err != nil {
		return nil, err
	}
	return &notiApi.GetServersResponse{Servers: server}, nil
}

// GetLeaderStatus implements the NotificationServer interface for retrieving the leader status of the server.
//
// This function determines whether the current server is the leader in the Raft cluster.
// It returns a boolean value indicating the leadership status. This method is primarily
// used for internal checks and monitoring within the distributed system.
func (n *notiServer) GetLeaderStatus(_ context.Context, _ *emptypb.Empty) (*notiApi.LeaderStatusResponse, error) {
	isLeader := n.ServerRetriever.IsLeader()
	return &notiApi.LeaderStatusResponse{IsLeader: isLeader}, nil
}

// NewGrpcServer creates a new gRPC server instance with the specified configuration and options.
func NewGrpcServer(config *NotiConfig, opts ...grpc.ServerOption) *grpc.Server {
	grpcServer := grpc.NewServer(opts...)

	srv := newNotiServer(config)
	hsrv := health.NewServer()
	hsrv.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)

	healthpb.RegisterHealthServer(grpcServer, hsrv)
	notiApi.RegisterNotificationServer(grpcServer, srv)

	return grpcServer
}
