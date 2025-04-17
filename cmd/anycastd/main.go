package main

import (
	"context"
	"net/http"
	"os"

	"github.com/kelseyhightower/envconfig"
	apipb "github.com/osrg/gobgp/v3/api"
	"github.com/osrg/gobgp/v3/pkg/server"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	log "github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"

	"github.com/runityru/anycastd/announcer"
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

type spec struct {
	ConfigPath string    `envconfig:"CONFIG_PATH" default:"/config.yaml"`
	LogLevel   log.Level `envconfig:"LOG_LEVEL" default:"WARN"`
}

func main() {
	ctx := context.TODO()

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
	bgpSrv := server.NewBgpServer(server.LoggerOption(&announcer.Logger{Logger: &log.Logger{
		Out:       os.Stderr,
		Formatter: lf,
		Hooks:     make(log.LevelHooks),
		Level:     s.LogLevel,
	}}))

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

	if err := bgpSrv.WatchEvent(context.Background(), &apipb.WatchEventRequest{
		Peer: &apipb.WatchEventRequest_Peer{},
	}, func(r *apipb.WatchEventResponse) {
		if p := r.GetPeer(); p != nil && p.Type == apipb.WatchEventResponse_PeerEvent_STATE {
			log.Info(p)
		}
	}); err != nil {
		panic(err)
	}

	for _, peer := range cfg.Announcer.Peers {
		err = bgpSrv.AddPeer(context.Background(), &apipb.AddPeerRequest{
			Peer: &apipb.Peer{
				Conf: &apipb.PeerConf{
					NeighborAddress: peer.RemoteAddress,
					PeerAsn:         peer.RemoteASN,
				},
				Timers: &apipb.Timers{
					Config: &apipb.TimersConfig{
						HoldTime:          uint64(peer.HoldTime.TimeDuration().Seconds()),
						KeepaliveInterval: uint64(peer.KeepaliveInterval.TimeDuration().Seconds()),
					},
				},
			},
		})
		if err != nil {
			panic(err)
		}
	}

	log.Info("Starting service initialization ...")
	for _, svcCfg := range cfg.Services {
		log.Tracef("Initializing service %s ...", svcCfg.Name)

		checks := []checkers.Checker{}
		for _, check := range svcCfg.Checks {
			log.WithFields(log.Fields{
				"service": svcCfg.Name,
				"check":   check.Kind,
			}).Trace("registering check ...")

			c, err := checkers.NewCheckerByKind(check.Kind, check.Spec)
			if err != nil {
				panic(err)
			}

			checks = append(checks, c)
		}

		a := announcer.New(announcer.Config{
			GoBGP:    bgpSrv,
			Prefixes: cfg.Announcer.Routes,
			NextHop:  cfg.Announcer.LocalAddress,
			LocalASN: cfg.Announcer.LocalASN,
		})

		metrics, err := service.NewMetrics(appVersion)
		if err != nil {
			panic(err)
		}

		svc := service.New(svcCfg.Name, a, checks, svcCfg.CheckInterval.TimeDuration(), metrics)

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
