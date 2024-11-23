/*
Package store implements a distributed key-value store using Raft consensus.

The store package provides a high-level API for managing a distributed system where multiple nodes can participate in decision-making through the Raft consensus protocol. It handles command application, state management, and cluster membership operations such as joining and leaving nodes.

Key Features:
- Distributed storage with Raft consensus
- Command handling for adding and broadcasting messages
- Read operations to retrieve stored messages
- Cluster management: joining new nodes and removing existing ones

Usage:

1. Initialize the DistributedStore with a data directory and configuration.
2. Use the AddCommand and BroadcastCommand methods to apply commands.
3. Retrieve stored messages using the Read and ReadLatest methods.
4. Manage cluster membership by joining or leaving nodes.
*/
package store

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/hashicorp/raft"
	wal "github.com/hashicorp/raft-wal"
	api "github.com/opplieam/bb-dist-noti/protogen/category_v1"
	notiApi "github.com/opplieam/bb-dist-noti/protogen/notification_v1"
)

// DistributedStore represents a distributed key-value store using Raft consensus.
type DistributedStore struct {
	config      Config
	dataDir     string
	raft        *raft.Raft
	fsm         *FiniteState
	logStore    *wal.WAL
	stableStore *wal.WAL
}

func NewDistributedStore(dataDir string, config Config) (*DistributedStore, error) {
	dis := &DistributedStore{
		config:  config,
		dataDir: dataDir,
	}
	if err := dis.setupRaft(); err != nil {
		return nil, err
	}
	return dis, nil
}

// setupRaft sets up the Raft instance and related components such as
// FSM, logStore, stableStore, snapshotStore, transport, and configures Raft.
func (d *DistributedStore) setupRaft() error {
	// Setup FSM
	fsm := NewFiniteState(d.config.FSM.Limit)
	d.fsm = fsm

	// Setup LogStore
	logStoreDir := filepath.Join(d.dataDir, "raft", "log")
	if err := os.MkdirAll(logStoreDir, 0755); err != nil {
		return fmt.Errorf("error creating raft log directory: %w", err)
	}
	logStore, err := wal.Open(logStoreDir)
	if err != nil {
		return fmt.Errorf("error opening raft log: %w", err)
	}
	d.logStore = logStore
	// Setup StableStore
	raftStableDir := filepath.Join(d.dataDir, "raft", "stable")
	if err = os.MkdirAll(raftStableDir, 0755); err != nil {
		return fmt.Errorf("error creating raft stable directory: %w", err)
	}
	stableStore, err := wal.Open(raftStableDir)
	if err != nil {
		return fmt.Errorf("error opening raft stable: %w", err)
	}
	d.stableStore = stableStore

	// Setup SnapshotStore
	// keep 1 snapshot
	retain := 1
	snapshotStore, err := raft.NewFileSnapshotStore(
		filepath.Join(d.dataDir, "raft"),
		retain,
		os.Stderr,
	)
	if err != nil {
		return fmt.Errorf("error creating snapshot store: %w", err)
	}

	// Setup Transport
	sl := NewStreamLayer(
		d.config.StreamLayer.RaftLn,
		d.config.StreamLayer.ServerTLSConfig,
		d.config.StreamLayer.PeerTLSConfig,
	)
	maxPool := d.config.TransportLayer.MaxPool
	timeout := d.config.TransportLayer.Timeout
	if maxPool <= 0 {
		maxPool = 5
	}
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	transport := raft.NewNetworkTransport(
		sl,
		maxPool,
		timeout,
		os.Stderr,
	)

	// Setup Config
	raftConfig := raft.DefaultConfig()
	// unique ID for this server
	raftConfig.LocalID = d.config.Raft.LocalID

	// Setup Raft
	d.raft, err = raft.NewRaft(
		raftConfig,
		fsm,
		logStore,
		stableStore,
		snapshotStore,
		transport,
	)
	if err != nil {
		return fmt.Errorf("error creating raft instance: %w", err)
	}

	hasState, err := raft.HasExistingState(logStore, stableStore, snapshotStore)
	if err != nil {
		return fmt.Errorf("error checking if log exists: %w", err)
	}
	if !hasState && d.config.Raft.Bootstrap {
		clusterConfig := raft.Configuration{
			Servers: []raft.Server{{
				ID:      raftConfig.LocalID,
				Address: raft.ServerAddress(d.config.Raft.Addr),
			}},
		}
		err = d.raft.BootstrapCluster(clusterConfig).Error()
		if err != nil {
			return fmt.Errorf("error bootstrap raft cluster: %w", err)
		}
	}
	return nil
}

// Close properly shuts down the DistributedStore instance and closing logStore and stableStore.
func (d *DistributedStore) Close() error {
	future := d.raft.Shutdown()
	if err := future.Error(); err != nil {
		return err
	}
	// Close WAL LogStore
	if err := d.logStore.Close(); err != nil {
		return err
	}
	// Close WAL StableStore
	if err := d.stableStore.Close(); err != nil {
		return err
	}
	return nil
}

// AddCommand applies an add command to the Raft consensus group.
func (d *DistributedStore) AddCommand(msg *api.CategoryMessage) error {
	b, err := newCommand(CommandTypeAdd, msg)
	if err != nil {
		return fmt.Errorf("error creating add command: %w", err)
	}
	if err = d.apply(b); err != nil {
		return fmt.Errorf("error applying add command: %w", err)
	}
	return nil
}

// BroadcastCommand applies a broadcast command to the Raft consensus group.
func (d *DistributedStore) BroadcastCommand(msg *api.CategoryMessage) error {
	b, err := newCommand(CommandTypeBroadcast, msg)
	if err != nil {
		return fmt.Errorf("error creating broadcast command: %w", err)
	}
	if err = d.apply(b); err != nil {
		return fmt.Errorf("error applying broadcast command: %w", err)
	}
	return nil
}

// apply sends a command to the Raft consensus group and waits for it to be applied.
func (d *DistributedStore) apply(cmd []byte) error {
	future := d.raft.Apply(cmd, 10*time.Second)
	if future.Error() != nil {
		return future.Error()
	}
	res := future.Response()
	if err, ok := res.(error); ok {
		return err
	}
	return nil
}

// Read retrieves the latest 'n' entries from the Finite State Machine.
func (d *DistributedStore) Read(n int) []*api.CategoryMessage {
	return d.fsm.Read(n)
}

// ReadLatest retrieves the most recent entry from the Finite State Machine.
func (d *DistributedStore) ReadLatest() *api.CategoryMessage {
	return d.fsm.ReadLatest()
}

// Join adds a new node with the given ID and address to the Raft cluster.
func (d *DistributedStore) Join(id, addr string) error {
	configFuture := d.raft.GetConfiguration()
	if err := configFuture.Error(); err != nil {
		return err
	}
	serverID := raft.ServerID(id)
	serverAddr := raft.ServerAddress(addr)
	for _, srv := range configFuture.Configuration().Servers {
		if srv.ID == serverID || srv.Address == serverAddr {
			if srv.ID == serverID && srv.Address == serverAddr {
				// server has already joined
				return nil
			}
			// remove the existing server, It might be conflict
			removeFuture := d.raft.RemoveServer(srv.ID, 0, 0)
			if err := removeFuture.Error(); err != nil {
				return err
			}
		}
	}
	addFuture := d.raft.AddVoter(serverID, serverAddr, 0, 0)
	if err := addFuture.Error(); err != nil {
		return err
	}
	return nil
}

// Leave removes a node with the given ID from the Raft cluster.
func (d *DistributedStore) Leave(id string) error {
	removeFuture := d.raft.RemoveServer(raft.ServerID(id), 0, 0)
	return removeFuture.Error()
}

// WaitForLeader waits until a leader is elected within the given timeout duration or returns an error if it times out.
func (d *DistributedStore) WaitForLeader(timeout time.Duration) error {
	timeoutC := time.After(timeout)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-timeoutC:
			return fmt.Errorf("timed out")
		case <-ticker.C:
			if le, _ := d.raft.LeaderWithID(); le != "" {
				return nil
			}
		}
	}
}

// GetServers returns a list of all servers in the Raft cluster. For each server,
// it provides the server ID, RPC address, and indicates whether it is the current
// leader. The function retrieves this information from the Raft configuration and
// leader state. Returns an error if unable to get the Raft configuration.
func (d *DistributedStore) GetServers() ([]*notiApi.Server, error) {
	future := d.raft.GetConfiguration()
	if err := future.Error(); err != nil {
		return nil, err
	}
	var servers []*notiApi.Server
	for _, server := range future.Configuration().Servers {
		leaderAddr, _ := d.raft.LeaderWithID()
		servers = append(servers, &notiApi.Server{
			Id:       string(server.ID),
			RpcAddr:  string(server.Address),
			IsLeader: leaderAddr == server.Address,
		})
	}
	return servers, nil
}
