package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/kelseyhightower/envconfig"
	apipb "github.com/osrg/gobgp/v4/api"
	"github.com/osrg/gobgp/v4/pkg/apiutil"
	"github.com/osrg/gobgp/v4/pkg/server"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	log "github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"

	"github.com/runityru/anycastd/announcer"
	"github.com/runityru/anycastd/announcer/gobgpv4"
	"github.com/runityru/anycastd/checkers"
	"github.com/runityru/anycastd/config"
	"github.com/runityru/anycastd/service"

	// checkers in build
	_ "github.com/runityru/anycastd/checkers/assigned_address"
	_ "github.com/runityru/anycastd/checkers/dns_lookup"
	_ "github.com/runityru/anycastd/checkers/http_2xx"
	_ "github.com/runityru/anycastd/checkers/icmp_ping"
	_ "github.com/runityru/anycastd/checkers/ntpq"
	_ "github.com/runityru/anycastd/checkers/tftp_rrq"
	_ "github.com/runityru/anycastd/checkers/tls_certificate"
)

var (
	appVersion     = "n/a (dev build)"
	buildTimestamp = "undefined"
)

// newPeer builds a gobgp peer definition. localAddress is set as the
// transport local address so the BGP session is sourced from the configured
// address instead of whatever the kernel picks by route lookup.
func newPeer(peer config.Peer, localAddress string) *apipb.Peer {
	return &apipb.Peer{
		Conf: &apipb.PeerConf{
			NeighborAddress: peer.RemoteAddress,
			PeerAsn:         peer.RemoteASN,
		},
		EbgpMultihop: &apipb.EbgpMultihop{
			Enabled:     peer.EnableMultihop,
			MultihopTtl: peer.MultihopTTL,
		},
		Transport: &apipb.Transport{
			LocalAddress: localAddress,
		},
	}
}

// slogLevel maps a logrus level to the corresponding slog level used to
// configure the GoBGP server logger.
func slogLevel(lvl log.Level) slog.Level {
	switch lvl {
	case log.TraceLevel, log.DebugLevel:
		return slog.LevelDebug
	case log.InfoLevel:
		return slog.LevelInfo
	case log.WarnLevel:
		return slog.LevelWarn
	default:
		return slog.LevelError
	}
}

type spec struct {
	ConfigPath string    `envconfig:"CONFIG_PATH" default:"/config.yaml"`
	LogLevel   log.Level `envconfig:"LOG_LEVEL" default:"WARN"`
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	s := spec{}
	envconfig.MustProcess("", &s)

	cfg, err := config.NewFromFile(s.ConfigPath)
	if err != nil {
		panic(err)
	}

	log.SetLevel(s.LogLevel)

	lf := new(log.TextFormatter)
	lf.FullTimestamp = true
	log.SetFormatter(lf)

	log.Infof("Initializing anycastd (%s @ %s) ...", appVersion, buildTimestamp)

	g, _ := errgroup.WithContext(ctx)

	log.Trace("Initializing BGP server ...")
	levelVar := new(slog.LevelVar)
	levelVar.Set(slogLevel(s.LogLevel))
	gobgpLogger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: levelVar,
	}))
	bgpSrv := server.NewBgpServer(server.LoggerOption(gobgpLogger, levelVar))

	g.Go(func() error {
		log.Trace("Starting to serve GoBGP API")
		bgpSrv.Serve()
		return nil
	})

	log.Trace("Starting BGP sessions ...")
	if err := bgpSrv.StartBgp(ctx, &apipb.StartBgpRequest{
		Global: &apipb.Global{
			RouterId:   cfg.Announcer.RouterID,
			Asn:        cfg.Announcer.LocalASN,
			ListenPort: -1,
		},
	}); err != nil {
		panic(err)
	}
	defer func() {
		err := bgpSrv.StopBgp(ctx, &apipb.StopBgpRequest{})
		if err != nil {
			log.Warnf("error stopping BGP session: %s", err)
		}
	}()

	if err := bgpSrv.WatchEvent(context.Background(), server.WatchEventMessageCallbacks{
		OnPeerUpdate: func(e *apiutil.WatchEventMessage_PeerEvent, _ time.Time) {
			if e.Type == apiutil.PEER_EVENT_STATE {
				log.Info(e)
			}
		},
	}, server.WatchPeer()); err != nil {
		panic(err)
	}

	for _, peer := range cfg.Announcer.Peers {
		err = bgpSrv.AddPeer(context.Background(), &apipb.AddPeerRequest{
			Peer: newPeer(peer, cfg.Announcer.LocalAddress),
		})
		if err != nil {
			panic(err)
		}
	}

	metrics, err := service.NewMetrics(appVersion)
	if err != nil {
		panic(err)
	}

	log.Info("Starting service initialization ...")
	for _, svcCfg := range cfg.Services {
		log.Tracef("Initializing service %s ...", svcCfg.Name)

		checks := []service.Checker{}
		for _, check := range svcCfg.Checks {
			log.WithFields(log.Fields{
				"service": svcCfg.Name,
				"check":   check.Kind,
			}).Trace("registering check ...")

			c, err := checkers.NewCheckerByKind(check.Kind, check.Spec)
			if err != nil {
				panic(err)
			}

			checks = append(checks, service.Checker{Check: c, Group: check.Group})
		}

		a := announcer.New(announcer.Config{
			GoBGP:    gobgpv4.NewAdapter(bgpSrv),
			Prefixes: cfg.Announcer.Routes,
			NextHop:  cfg.Announcer.LocalAddress,
		})

		strategy, err := service.GetStrategy(svcCfg.Strategy, svcCfg.StrategyOptions)
		if err != nil {
			panic(err)
		}

		svc := service.New(svcCfg.Name, a, checks, svcCfg.CheckInterval.TimeDuration(), metrics, strategy)

		g.Go(func() error {
			return svc.Run(ctx)
		})
	}

	if cfg.Metrics.Enabled {
		log.Debug("metrics server is enabled, initializing ...")

		g.Go(func() error {
			http.Handle("/metrics", promhttp.Handler())
			return http.ListenAndServe(cfg.Metrics.Address, nil)
		})

		amr := announcer.NewMetricsRepository(bgpSrv, cfg.Announcer.RouterID, cfg.Announcer.LocalASN)
		if err := amr.Register(); err != nil {
			panic(err)
		}

		g.Go(func() error {
			return amr.Run(ctx)
		})
	}

	log.Infof("Initialization completed")

	if err := g.Wait(); err != nil {
		panic(err)
	}
}
