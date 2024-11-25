package agent_test

import (
	"context"
	"crypto/tls"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/opplieam/bb-dist-noti/internal/agent"
	"github.com/opplieam/bb-dist-noti/internal/tlsconfig"
	"github.com/opplieam/bb-dist-noti/pkg/dynaport"
	notiApi "github.com/opplieam/bb-dist-noti/protogen/notification_v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// TestAgent tests the behavior of the agent package, including leader election and shutdown.
func TestAgent(t *testing.T) {
	// Setup TLS configurations for server and peer
	serverTLSConfig, err := tlsconfig.SetupTLSConfig(tlsconfig.TLSConfig{
		CertFile:      tlsconfig.ServerCertFile,
		KeyFile:       tlsconfig.ServerKeyFile,
		CAFile:        tlsconfig.CAFile,
		Server:        true,
		ServerAddress: "127.0.0.1",
	})
	require.NoError(t, err)
	peerTLSConfig, err := tlsconfig.SetupTLSConfig(tlsconfig.TLSConfig{
		CertFile:      tlsconfig.ClientCertFile,
		KeyFile:       tlsconfig.ClientKeyFile,
		CAFile:        tlsconfig.CAFile,
		Server:        false,
		ServerAddress: "127.0.0.1",
	})
	require.NoError(t, err)

	var agents []*agent.Agent
	for i := 0; i < 3; i++ {
		// Get two dynamic ports for serf and rpc communication
		ports := dynaport.Get(2)
		serfAddr := fmt.Sprintf("%s:%d", "127.0.0.1", ports[0])
		rpcPort := ports[1]

		dataDir, err := os.MkdirTemp("", "agent-test")
		require.NoError(t, err)

		// Set start join addresses for agents 1 and 2 to join the first agent (leader)
		var startJoinAddrs []string
		if i != 0 {
			startJoinAddrs = append(startJoinAddrs, agents[0].Config.SerfAddr)
		}
		a, err := agent.NewAgent(agent.Config{
			ServerTLSConfig: serverTLSConfig,
			PeerTLSConfig:   peerTLSConfig,
			DataDir:         dataDir,
			StartJoinAddrs:  startJoinAddrs,
			SerfAddr:        serfAddr,
			RPCPort:         rpcPort,
			NodeName:        fmt.Sprintf("%d", i),
			Bootstrap:       i == 0,
		})
		require.NoError(t, err)
		agents = append(agents, a)
	}
	// Defer shutdown of all agents and remove their temporary data directories after the test
	defer func() {
		for _, a := range agents {
			err := a.Shutdown()
			require.NoError(t, err)
			require.NoError(t, os.RemoveAll(a.Config.DataDir))
		}
	}()

	// Wait until agent 0 becomes the leader
	client1 := newClient(t, agents[1], peerTLSConfig)
	require.Eventually(t, func() bool {
		serverListResponse, err := client1.GetServers(context.Background(), &notiApi.GetServersRequest{})
		if err != nil {
			return false
		}
		servers := serverListResponse.Servers
		// Check if there are 3 servers and agent 0 is the leader
		return len(servers) == 3 &&
			!servers[1].IsLeader && !servers[2].IsLeader &&
			servers[0].IsLeader
	}, 3*time.Second, time.Millisecond*100)

	// Shut down agent 0 to simulate a failure and trigger a leader election
	err = agents[0].Shutdown()
	require.NoError(t, err)

	// Wait until either agent 1 or agent 2 becomes the new leader after agent 0 is shut down
	require.Eventually(t, func() bool {
		serverListResponse, err := client1.GetServers(context.Background(), &notiApi.GetServersRequest{})
		if err != nil {
			return false
		}
		// Check that the new leader is one of agents 1 or 2 and not agent 0
		servers := serverListResponse.Servers
		return !servers[0].IsLeader && (servers[1].IsLeader || servers[2].IsLeader)
	}, 5*time.Second, time.Millisecond*100)

}

// newClient creates a gRPC client connected to the specified agent.
func newClient(t *testing.T, agent *agent.Agent, tlsConfig *tls.Config) notiApi.NotificationClient {
	// Create TLS credentials using the provided TLS configuration
	tlsCreds := credentials.NewTLS(tlsConfig)
	// Set up gRPC dial options with transport credentials
	opts := []grpc.DialOption{grpc.WithTransportCredentials(tlsCreds)}
	// Get the RPC address for the agent
	rpcAddr, err := agent.Config.RPCAddr()
	require.NoError(t, err)
	// Dial the gRPC server at the RPC address
	conn, err := grpc.NewClient(rpcAddr, opts...)
	require.NoError(t, err)
	return notiApi.NewNotificationClient(conn)
}
