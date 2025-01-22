package store_test

import (
	"fmt"
	"net"
	"os"
	"testing"
	"time"

	"github.com/hashicorp/raft"
	"github.com/opplieam/bb-dist-noti/internal/store"
	"github.com/opplieam/bb-dist-noti/pkg/dynaport"
	api "github.com/opplieam/bb-dist-noti/protogen/category_v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func setupNodes(t *testing.T, disStores []*store.DistributedStore, nodeCount int) []*store.DistributedStore {
	ports := dynaport.Get(nodeCount)
	for i := 0; i < nodeCount; i++ {
		dataDir, err := os.MkdirTemp("", "distributed-store-test")
		require.NoError(t, err)
		defer func(dir string) {
			_ = os.RemoveAll(dir)
		}(dataDir)

		ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", ports[i]))
		require.NoError(t, err)

		config := store.Config{}
		config.StreamLayer.RaftLn = ln
		config.StreamLayer.ServerTLSConfig = nil
		config.StreamLayer.PeerTLSConfig = nil

		config.Raft.LocalID = raft.ServerID(fmt.Sprintf("%d", i))
		config.Raft.Addr = ln.Addr().String()

		if i == 0 {
			config.Raft.Bootstrap = true
		}

		d, err := store.NewDistributedStore(dataDir, config)
		require.NoError(t, err)

		if i != 0 {
			err = disStores[0].Join(fmt.Sprintf("%d", i), ln.Addr().String())
			require.NoError(t, err)
		} else {
			err = d.WaitForLeader(3 * time.Second)
			require.NoError(t, err)
		}
		disStores = append(disStores, d)
	}
	return disStores
}

func TestMultipleNodes(t *testing.T) {
	var disStores []*store.DistributedStore
	nodeCount := 3
	disStores = setupNodes(t, disStores, nodeCount)

	catInfo := []*api.CategoryMessage{
		{UserId: 1, CategoryFrom: "Phone", CategoryTo: "Mobile"},
		{UserId: 2, CategoryFrom: "Game", CategoryTo: "Video game"},
	}

	// Test replication
	for _, cat := range catInfo {
		msgB, err := proto.Marshal(cat)
		require.NoError(t, err)
		err = disStores[0].AddCommand(msgB)
		require.NoError(t, err)

		require.Eventually(t, func() bool {
			for j := 0; j < nodeCount; j++ {
				gotReadLatest := disStores[j].ReadLatest()
				if gotReadLatest == nil {
					return false
				}
				if !proto.Equal(gotReadLatest, cat) {
					return false
				}
			}
			return true
		}, 500*time.Millisecond, 50*time.Millisecond)
	}

	// Test Read on the follower node
	gotRead := disStores[2].Read(2)
	require.Equal(t, len(catInfo), len(gotRead))
	for i := range catInfo {
		require.True(t, proto.Equal(gotRead[i], catInfo[i]))
	}

	// Test Leader status
	servers, err := disStores[0].GetServers()
	require.NoError(t, err)
	require.Equal(t, 3, len(servers))
	require.True(t, servers[0].IsLeader)
	require.False(t, servers[1].IsLeader)
	require.False(t, servers[2].IsLeader)

	// Test Node Leaving
	err = disStores[0].Leave("1")
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		servers, err = disStores[0].GetServers()
		if err != nil && len(servers) != 2 && !servers[0].IsLeader && servers[1].IsLeader {
			return false
		}
		return true
	}, 200*time.Millisecond, 50*time.Millisecond)

	// Test don't replicate if the node leave
	moreCatInfo := &api.CategoryMessage{
		UserId:       3,
		CategoryFrom: "Car Truck",
		CategoryTo:   "Truck",
	}
	msgB, err := proto.Marshal(moreCatInfo)
	require.NoError(t, err)
	err = disStores[0].AddCommand(msgB)
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		got := disStores[1].ReadLatest()
		if proto.Equal(got, moreCatInfo) {
			return false
		}
		got2 := disStores[2].ReadLatest()
		if !proto.Equal(got2, moreCatInfo) {
			return false
		}
		return true
	}, 200*time.Millisecond, 50*time.Millisecond)
}
