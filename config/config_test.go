package config

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
	th "github.com/teran/go-time"
)

func TestConfig(t *testing.T) {
	type testCase struct {
		name       string
		samplePath string
		expOut     Config
		expError   error
	}

	sampleConfig := Config{
		Announcer: Announcer{
			RouterID:     "10.3.3.3",
			LocalAddress: "10.0.0.1",
			LocalASN:     65999,
			Routes:       []string{"10.0.0.128/32"},
			Peers: []Peer{
				{
					Name:              "some_router_1",
					RemoteAddress:     "10.0.0.252",
					RemoteASN:         65000,
					HoldTime:          th.Duration(30 * time.Second),
					KeepaliveInterval: th.Duration(10 * time.Second),
				},
				{
					Name:              "some_router_2",
					RemoteAddress:     "10.0.0.253",
					RemoteASN:         65000,
					HoldTime:          th.Duration(60 * time.Second),
					KeepaliveInterval: th.Duration(5 * time.Second),
				},
			},
		},
		Services: []Service{
			{
				Name:          "http",
				CheckInterval: th.Duration(10 * time.Second),
				Checks: []Check{
					{
						Kind: "dns_lookup",
						Spec: json.RawMessage(`{"interval":"100ms","query":"example.com","resolver":"127.0.0.1","tries":3}`),
					},
					{
						Kind: "http_2xx",
						Spec: json.RawMessage(`{"address":"127.0.0.1:8080","interval":"100ms","path":"/","timeout":"2s","tries":3}`),
					},
					{
						Kind: "assigned_address",
						Spec: json.RawMessage(`{"interface":"dummy0","ipv4":"10.0.0.128"}`),
					},
				},
			},
		},
		Metrics: Metrics{
			Enabled: true,
			Address: "127.0.0.1:9090",
		},
	}

	tcs := []testCase{
		{
			name:       "YAML configuration",
			samplePath: "testdata/sample.yaml",
			expOut:     sampleConfig,
		},
		{
			name:       "JSON configuration",
			samplePath: "testdata/sample.json",
			expOut:     sampleConfig,
		},
		{
			name:       "wrong config file extension",
			samplePath: "testdata/sample.unknown",
			expError:   errors.New("unexpected file format: `.unknown`"),
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)

			cfg, err := NewFromFile(tc.samplePath)
			if tc.expError == nil {
				r.NoError(err)
				r.Equal(tc.expOut.Announcer, cfg.Announcer)
				for i := range tc.expOut.Services {
					r.Equalf(tc.expOut.Services[i].Name, cfg.Services[i].Name, "svc#%d", i)
					r.Equalf(tc.expOut.Services[i].CheckInterval, cfg.Services[i].CheckInterval, "svc#%d", i)
					for j := range tc.expOut.Services[i].Checks {
						r.Equalf(tc.expOut.Services[i].Checks[j].Kind, cfg.Services[i].Checks[j].Kind, "svc#%d check#%d", i, j)
						r.JSONEqf(string(tc.expOut.Services[i].Checks[j].Spec), string(cfg.Services[i].Checks[j].Spec), "svc#%d check#%d", i, j)
					}
				}
				r.Equal(tc.expOut.Metrics, cfg.Metrics)
			} else {
				r.Error(err)
				r.Equal(tc.expError.Error(), err.Error())
			}
		})
	}
}

func TestEmptyConfig(t *testing.T) {
	r := require.New(t)

	_, err := NewFromFile("testdata/empty.yaml")
	r.Error(err)
	r.Equal(
		"announcer: (local_address: cannot be blank; local_asn: cannot be blank; peers: cannot be blank; router_id: cannot be blank; routes: cannot be blank.); metrics: (address: cannot be blank; enabled: cannot be blank.); services: cannot be blank.",
		err.Error(),
	)
}

func TestEmptyAnnouncerOnlyConfig(t *testing.T) {
	r := require.New(t)

	// Actually there are no semantics checks so it should pass
	_, err := NewFromFile("testdata/empty_with_announcer.yaml")
	r.NoError(err)
}
