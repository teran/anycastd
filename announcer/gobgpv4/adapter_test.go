package gobgpv4

import (
	"context"
	"testing"

	apipb "github.com/osrg/gobgp/v4/api"
	"github.com/osrg/gobgp/v4/pkg/apiutil"
	"github.com/osrg/gobgp/v4/pkg/packet/bgp"
	"github.com/osrg/gobgp/v4/pkg/server"
	"github.com/stretchr/testify/require"
)

func TestNewPath(t *testing.T) {
	t.Run("with next hop", func(t *testing.T) {
		r := require.New(t)

		p, err := newPath("10.0.0.0/24", "192.168.0.1")
		r.NoError(err)
		r.NotNil(p)

		r.Equal(bgp.RF_IPv4_UC, p.Family)
		r.IsType(&bgp.IPAddrPrefix{}, p.Nlri)
		r.Len(p.Attrs, 2)

		hasNextHop := false
		for _, a := range p.Attrs {
			if _, ok := a.(*bgp.PathAttributeNextHop); ok {
				hasNextHop = true
			}
		}
		r.True(hasNextHop, "expected a *bgp.PathAttributeNextHop in attributes")
	})

	t.Run("without next hop", func(t *testing.T) {
		r := require.New(t)

		p, err := newPath("10.0.0.0/24", "")
		r.NoError(err)
		r.NotNil(p)

		r.Equal(bgp.RF_IPv4_UC, p.Family)
		r.Len(p.Attrs, 1)
	})

	t.Run("invalid prefix", func(t *testing.T) {
		r := require.New(t)

		_, err := newPath("not-a-prefix", "192.168.0.1")
		r.Error(err)
	})

	t.Run("invalid next hop", func(t *testing.T) {
		r := require.New(t)

		_, err := newPath("10.0.0.0/24", "not-an-address")
		r.Error(err)
	})
}

func TestNewWithdrawalPath(t *testing.T) {
	t.Run("marks path as withdrawal", func(t *testing.T) {
		r := require.New(t)

		p, err := newWithdrawalPath("10.0.0.0/24")
		r.NoError(err)
		r.NotNil(p)

		r.True(p.Withdrawal, "expected the path to be flagged as a withdrawal")
		r.Equal(bgp.RF_IPv4_UC, p.Family)
		// Withdrawal paths carry no next-hop attribute; they must not fail
		// the nexthop check in gobgp's apiutil2Path.
		r.Len(p.Attrs, 1)
	})

	t.Run("invalid prefix", func(t *testing.T) {
		r := require.New(t)

		_, err := newWithdrawalPath("not-a-prefix")
		r.Error(err)
	})
}

// TestDeletePathWithdrawsOnRealServer runs a real in-process GoBGP v4 server
// through the adapter and proves that a DeletePath (withdrawal) actually
// succeeds — the seam that previously failed with "nexthop not found" because
// the path was not flagged as a withdrawal. This is the unit-level equivalent
// of the docker e2e's withdrawal assertion, and works without Docker.
func TestDeletePathWithdrawsOnRealServer(t *testing.T) {
	ctx := context.Background()

	srv := server.NewBgpServer()
	go srv.Serve()

	require.NoError(t, srv.StartBgp(ctx, &apipb.StartBgpRequest{
		Global: &apipb.Global{
			RouterId:   "10.0.0.1",
			Asn:        65000,
			ListenPort: -1,
		},
	}))
	t.Cleanup(func() { _ = srv.StopBgp(ctx, &apipb.StopBgpRequest{}) })

	a := NewAdapter(srv)

	// AddPath with a next hop must succeed.
	require.NoError(t, a.AddPath(ctx, "192.0.2.0/24", "10.0.0.1"))

	// DeletePath (withdrawal) must succeed. Historically this failed with
	// "nexthop not found" because the path had no next-hop and was not flagged
	// as a withdrawal.
	require.NoError(t, a.DeletePath(ctx, "192.0.2.0/24"))

	// Negative control: a non-withdrawal path with an empty next-hop reproduces
	// the original failure, confirming the Withdrawal flag is what fixes it.
	p, err := newPath("192.0.2.0/24", "")
	require.NoError(t, err)
	require.Error(t, srv.DeletePath(apiutil.DeletePathRequest{
		Paths: []*apiutil.Path{p},
	}))
}
