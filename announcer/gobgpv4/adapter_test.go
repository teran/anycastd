package gobgpv4

import (
	"testing"

	"github.com/osrg/gobgp/v4/pkg/packet/bgp"
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
