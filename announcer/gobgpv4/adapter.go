// Package gobgpv4 provides a GoBGP v4 implementation of the announcer.GoBGPServer interface.
package gobgpv4

import (
	"context"
	"net/netip"

	"github.com/osrg/gobgp/v4/pkg/apiutil"
	"github.com/osrg/gobgp/v4/pkg/packet/bgp"
	"github.com/osrg/gobgp/v4/pkg/server"

	"github.com/runityru/anycastd/announcer"
)

type adapter struct {
	srv *server.BgpServer
}

// NewAdapter wraps a GoBGP v4 *server.BgpServer into the
// application-level announcer.GoBGPServer interface, isolating all
// GoBGP-specific types from the rest of the code.
func NewAdapter(srv *server.BgpServer) announcer.GoBGPServer {
	return &adapter{srv: srv}
}

func (a *adapter) AddPath(ctx context.Context, prefix, nextHop string) error {
	// ctx is unused: the v4 AddPath API has no context parameter.
	p, err := newPath(prefix, nextHop)
	if err != nil {
		return err
	}

	resps, err := a.srv.AddPath(apiutil.AddPathRequest{
		Paths: []*apiutil.Path{p},
	})
	if err != nil {
		return err
	}

	for _, resp := range resps {
		if resp.Error != nil {
			return resp.Error
		}
	}

	return nil
}

func (a *adapter) DeletePath(ctx context.Context, prefix string) error {
	// ctx is unused: the v4 DeletePath API has no context parameter.
	// A withdrawal path carries only the NLRI — it must be flagged
	// Withdrawal so apiutil2Path skips the next-hop lookup (an empty nextHop
	// would otherwise make it fail with "nexthop not found").
	p, err := newWithdrawalPath(prefix)
	if err != nil {
		return err
	}

	return a.srv.DeletePath(apiutil.DeletePathRequest{
		Paths: []*apiutil.Path{p},
	})
}

// newWithdrawalPath builds a GoBGP v4 *apiutil.Path that withdraws the given
// prefix. It reuses newPath with an empty next-hop (withdrawal paths carry no
// nexthop attribute) and marks the path as a withdrawal.
func newWithdrawalPath(prefix string) (*apiutil.Path, error) {
	p, err := newPath(prefix, "")
	if err != nil {
		return nil, err
	}

	p.Withdrawal = true
	return p, nil
}

// newPath builds a GoBGP v4 *apiutil.Path for the given prefix and nextHop.
func newPath(prefix, nextHop string) (*apiutil.Path, error) {
	p, err := netip.ParsePrefix(prefix)
	if err != nil {
		return nil, err
	}

	nlri, err := bgp.NewIPAddrPrefix(p)
	if err != nil {
		return nil, err
	}

	attrs := []bgp.PathAttributeInterface{
		bgp.NewPathAttributeOrigin(0),
	}
	if nextHop != "" {
		nh, err := netip.ParseAddr(nextHop)
		if err != nil {
			return nil, err
		}

		a2, err := bgp.NewPathAttributeNextHop(nh)
		if err != nil {
			return nil, err
		}
		attrs = append(attrs, a2)
	}

	return &apiutil.Path{
		Family: bgp.RF_IPv4_UC,
		Nlri:   nlri,
		Attrs:  attrs,
	}, nil
}
