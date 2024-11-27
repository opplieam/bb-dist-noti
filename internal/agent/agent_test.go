package agent_test

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/opplieam/bb-dist-noti/internal/agent"
	"github.com/opplieam/bb-dist-noti/internal/clientstate"
	"github.com/opplieam/bb-dist-noti/internal/httpserver"
	"github.com/opplieam/bb-dist-noti/pkg/dynaport"
	api "github.com/opplieam/bb-dist-noti/protogen/category_v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

type testCluster struct {
	t          *testing.T
	natsServer *server.Server
	agents     []*agent.Agent
	baseDir    string
}

func setupNatsServer(t *testing.T) *server.Server {
	opts := &server.Options{
		Host:           "127.0.0.1",
		Port:           -1, // random port
		NoLog:          true,
		NoSigs:         true,
		MaxControlLine: 256,
		JetStream:      true, // Enable JetStream
	}

	svr, err := server.NewServer(opts)
	require.NoError(t, err)

	// Start the server
	svr.Start()

	// Wait for server to be ready
	if !svr.ReadyForConnections(4 * time.Second) {
		t.Fatal("NATS server failed to start")
	}

	return svr
}

func createTestCluster(t *testing.T, nodeCount int) *testCluster {
	baseTmpDir := t.TempDir()
	natsServer := setupNatsServer(t)

	var agents []*agent.Agent
	for i := 0; i < nodeCount; i++ {
		ports := dynaport.Get(3)
		serfPort := ports[0]
		rpcPort := ports[1]
		httpPort := ports[2]

		dataDir := filepath.Join(baseTmpDir, fmt.Sprintf("node-%d", i))
		err := os.MkdirAll(dataDir, 0755)
		require.NoError(t, err)

		var startJoinAddrs []string
		if i != 0 {
			startJoinAddrs = append(startJoinAddrs, agents[0].Config.SerfAddr)
		}

		agentCfg := agent.Config{
			NodeName:  fmt.Sprintf("%d", i),
			DataDir:   dataDir,
			Bootstrap: i == 0,
			HttpConfig: httpserver.Config{
				//Addr:   fmt.Sprintf(":%d", BaseHttpPort+i),
				Addr:   fmt.Sprintf(":%d", httpPort),
				CState: clientstate.NewClientState(),
			},
			NatsAddr:       fmt.Sprintf("nats://%s", natsServer.Addr().String()),
			SerfAddr:       fmt.Sprintf("127.0.0.1:%d", serfPort),
			RPCPort:        rpcPort,
			StartJoinAddrs: startJoinAddrs,
			HistorySize:    1000,
		}
		a, err := agent.NewAgent(agentCfg)
		require.NoError(t, err)
		agents = append(agents, a)
		time.Sleep(2 * time.Second)
	}
	return &testCluster{
		t:          t,
		natsServer: natsServer,
		agents:     agents,
		baseDir:    baseTmpDir,
	}
}

func (tc *testCluster) cleanup() {
	for _, a := range tc.agents {
		a.Shutdown()
	}
	tc.natsServer.Shutdown()
}

func (tc *testCluster) publishTestMessage(msg *api.CategoryMessage) error {
	nc, err := nats.Connect(fmt.Sprintf("nats://%s", tc.natsServer.Addr().String()))
	if err != nil {
		return err
	}
	defer nc.Close()

	js, err := jetstream.New(nc)
	if err != nil {
		return err
	}

	// Create stream if it doesn't exist
	_, err = js.CreateOrUpdateStream(context.Background(), jetstream.StreamConfig{
		Name: "jobs",
		Subjects: []string{
			"jobs.noti.>",
		},
	})
	if err != nil {
		return err
	}

	msgBytes, err := proto.Marshal(msg)
	if err != nil {
		return err
	}

	_, err = js.Publish(context.Background(), "jobs.noti.change", msgBytes)
	return err
}

func connectSSE(t *testing.T, httpAddr string) (*http.Response, error) {
	client := &http.Client{}
	req, err := http.NewRequest("GET",
		fmt.Sprintf("http://127.0.0.1%s/category", httpAddr),
		nil)
	if err != nil {
		return nil, err
	}

	resp, err := client.Do(req)

	return resp, err
}

func verifySSEMessage(t *testing.T, resp *http.Response, expectedMsg *api.CategoryMessage) {
	messageReceived := make(chan bool, 1)
	var receivedErr error

	go func() {
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			// Skip if not a data line
			if !strings.HasPrefix(line, "data: ") {
				continue
			}

			expected := fmt.Sprintf(
				"data: UserID: %d Category: %s MatchCategory: %s",
				expectedMsg.UserId, expectedMsg.CategoryFrom, expectedMsg.CategoryTo,
			)
			if expected != line {
				messageReceived <- false
			} else {
				messageReceived <- true
			}
		}

		if err := scanner.Err(); err != nil {
			receivedErr = fmt.Errorf("SSE scanner error: %v", err)
			messageReceived <- false
		}
	}()

	// Wait for message with timeout
	select {
	case success := <-messageReceived:
		require.True(t, success)
		if !success {
			t.Errorf("Failed to verify SSE message: %v", receivedErr)
		}
	case <-time.After(5 * time.Second):
		t.Error("Timeout waiting for SSE message")
	}
}

func TestReplication(t *testing.T) {
	cluster := createTestCluster(t, 3)
	defer cluster.cleanup()

	// Wait for cluster to stabilize
	time.Sleep(3 * time.Second)

	// Connect SSE to follower node, agents[1] and agents[2]
	var responses []*http.Response
	for i := 1; i < len(cluster.agents); i++ {
		resp, err := connectSSE(t, cluster.agents[i].Config.HttpConfig.Addr)
		require.NoError(t, err)
		defer resp.Body.Close()
		responses = append(responses, resp)
	}

	// Publish test message
	testMsg := &api.CategoryMessage{
		UserId:       123,
		CategoryFrom: "electronics",
		CategoryTo:   "computers",
	}
	err := cluster.publishTestMessage(testMsg)
	require.NoError(t, err)

	for _, resp := range responses {
		verifySSEMessage(t, resp, testMsg)
	}
}

func TestFaultTolerance(t *testing.T) {
	cluster := createTestCluster(t, 3)
	defer cluster.cleanup()

	// Wait for cluster to stabilize
	time.Sleep(3 * time.Second)

	// Shutdown the leader
	cluster.agents[0].Shutdown()

	// Allow time for new leader election
	time.Sleep(5 * time.Second)

	// Connect to a remaining node
	resp, err := connectSSE(t, cluster.agents[1].Config.HttpConfig.Addr)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Publish test message
	testMsg := &api.CategoryMessage{
		UserId:       456,
		CategoryFrom: "books",
		CategoryTo:   "ebooks",
	}
	err = cluster.publishTestMessage(testMsg)
	require.NoError(t, err)
	verifySSEMessage(t, resp, testMsg)
}
