package discovery_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/hashicorp/serf/serf"
	"github.com/opplieam/bb-dist-noti/internal/discovery"
	"github.com/opplieam/bb-dist-noti/internal/dynaport"
	"github.com/stretchr/testify/require"
)

// Mock handler
type handler struct {
	joins  chan map[string]string
	leaves chan string
}

func (h *handler) Join(id, addr string) error {
	if h.joins != nil {
		h.joins <- map[string]string{
			"id":   id,
			"addr": addr,
		}
	}
	return nil
}

func (h *handler) Leave(id string) error {
	if h.leaves != nil {
		h.leaves <- id
	}
	return nil
}

func TestMembership(t *testing.T) {
	m, ha := setupMember(t, nil)
	m, _ = setupMember(t, m)
	m, _ = setupMember(t, m)

	require.Eventually(t, func() bool {
		return 2 == len(ha.joins) &&
			3 == len(m[0].Members()) &&
			0 == len(ha.leaves)
	}, 3*time.Second, 250*time.Millisecond)

	require.NoError(t, m[2].Leave())

	require.Eventually(t, func() bool {
		return 2 == len(ha.joins) &&
			3 == len(m[0].Members()) &&
			serf.StatusLeft == m[0].Members()[2].Status &&
			1 == len(ha.leaves)
	}, 3*time.Second, 250*time.Millisecond)

	require.Equal(t, fmt.Sprintf("%d", 2), <-ha.leaves)
}

func setupMember(t *testing.T, members []*discovery.Membership) ([]*discovery.Membership, *handler) {
	id := len(members)
	ports := dynaport.Get(1)
	addr := fmt.Sprintf("127.0.0.1:%d", ports[0])
	tags := map[string]string{
		"rpc_addr": addr,
	}

	c := discovery.Config{
		NodeName: fmt.Sprintf("%d", id),
		SerfAddr: addr,
		Tags:     tags,
	}

	h := &handler{}
	if len(members) == 0 {
		h.joins = make(chan map[string]string, 3)
		h.leaves = make(chan string, 3)
	} else {
		c.StartJoinAddr = []string{
			members[0].SerfAddr,
		}
	}

	m, err := discovery.New(h, c)
	require.NoError(t, err)
	members = append(members, m)

	return members, h
}
