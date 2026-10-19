package announcer

import (
	"context"
	"time"

	apipb "github.com/osrg/gobgp/v4/api"
	"github.com/osrg/gobgp/v4/pkg/server"
	"github.com/prometheus/client_golang/prometheus"
)

type Metrics interface {
	Run(ctx context.Context) error
	Register() error
}

type metrics struct {
	peerCount        *prometheus.GaugeVec
	peerAdminState   *prometheus.GaugeVec
	peerSessionState *prometheus.GaugeVec

	receivedMessagesTotal          *prometheus.GaugeVec
	receivedMessagesNotification   *prometheus.GaugeVec
	receivedMessagesUpdate         *prometheus.GaugeVec
	receivedMessagesOpen           *prometheus.GaugeVec
	receivedMessagesKeepalive      *prometheus.GaugeVec
	receivedMessagesRefresh        *prometheus.GaugeVec
	receivedMessagesWithdrawUpdate *prometheus.GaugeVec
	receivedMessagesWithdrawPrefix *prometheus.GaugeVec

	sentMessagesTotal          *prometheus.GaugeVec
	sentMessagesNotification   *prometheus.GaugeVec
	sentMessagesUpdate         *prometheus.GaugeVec
	sentMessagesOpen           *prometheus.GaugeVec
	sentMessagesKeepalive      *prometheus.GaugeVec
	sentMessagesRefresh        *prometheus.GaugeVec
	sentMessagesWithdrawUpdate *prometheus.GaugeVec
	sentMessagesWithdrawPrefix *prometheus.GaugeVec

	bgpPeerOutQueueCount     *prometheus.GaugeVec
	bgpPeerFlopsCount        *prometheus.GaugeVec
	bgpPeerSendCommunityFlag *prometheus.GaugeVec
	bgpPeerRemovePrivateFlag *prometheus.GaugeVec
	bgpPeerPasswordSetFlag   *prometheus.GaugeVec
	bgpPeerType              *prometheus.GaugeVec

	bgpSrv   *server.BgpServer
	routerID string
	asn      uint32
}

func NewMetricsRepository(bgpSrv *server.BgpServer, routerID string, asn uint32) Metrics {
	m := &metrics{
		peerCount: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "anycastd",
				Subsystem: "gobgp",
				Name:      "peer_count",
				Help:      "Total amount of peers configured for the GoBGP instance",
			},
			[]string{"router_id"},
		),
		peerAdminState: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "anycastd",
				Subsystem: "gobgp",
				Name:      "peer_admin_state",
				Help:      "Peer state 0=up, 1=down, 2=pfx_ct",
			},
			[]string{"router_id", "peer"},
		),
		peerSessionState: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "anycastd",
				Subsystem: "gobgp",
				Name:      "peer_session_state",
				Help:      "Peer session state 0=unknown, 1=idle, 2=connect, 3=active, 4=opensent, 5=openconfirm, 6=established",
			},
			[]string{"router_id", "peer"},
		),

		receivedMessagesTotal: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "anycastd",
				Subsystem: "gobgp",
				Name:      "received_messages_total",
				Help:      "Total number of messages received from the peer",
			},
			[]string{"router_id", "peer"},
		),
		receivedMessagesNotification: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "anycastd",
				Subsystem: "gobgp",
				Name:      "received_messages_notification",
				Help:      "Number of Notification messages received from the peer",
			},
			[]string{"router_id", "peer"},
		),
		receivedMessagesUpdate: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "anycastd",
				Subsystem: "gobgp",
				Name:      "received_messages_update",
				Help:      "Number of Update messages received from the peer",
			},
			[]string{"router_id", "peer"},
		),
		receivedMessagesOpen: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "anycastd",
				Subsystem: "gobgp",
				Name:      "received_messages_open",
				Help:      "Number of Open messages received from the peer",
			},
			[]string{"router_id", "peer"},
		),
		receivedMessagesKeepalive: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "anycastd",
				Subsystem: "gobgp",
				Name:      "received_messages_keepalive",
				Help:      "Number of Keepalive messages received from the peer",
			},
			[]string{"router_id", "peer"},
		),
		receivedMessagesRefresh: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "anycastd",
				Subsystem: "gobgp",
				Name:      "received_messages_refresh",
				Help:      "Number of Refresh messages received from the peer",
			},
			[]string{"router_id", "peer"},
		),
		receivedMessagesWithdrawUpdate: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "anycastd",
				Subsystem: "gobgp",
				Name:      "received_messages_withdraw_update",
				Help:      "Number of Withdraw Update messages received from the peer",
			},
			[]string{"router_id", "peer"},
		),
		receivedMessagesWithdrawPrefix: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "anycastd",
				Subsystem: "gobgp",
				Name:      "received_messages_withdraw_prefix",
				Help:      "Number of Withdraw Prefix messages received from the peer",
			},
			[]string{"router_id", "peer"},
		),

		sentMessagesTotal: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "anycastd",
				Subsystem: "gobgp",
				Name:      "sent_messages_total",
				Help:      "Total number of messages sent to the peer",
			},
			[]string{"router_id", "peer"},
		),
		sentMessagesNotification: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "anycastd",
				Subsystem: "gobgp",
				Name:      "sent_messages_notification",
				Help:      "Number of Notification messages sent to the peer",
			},
			[]string{"router_id", "peer"},
		),
		sentMessagesUpdate: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "anycastd",
				Subsystem: "gobgp",
				Name:      "sent_messages_update",
				Help:      "Number of Update messages sent to the peer",
			},
			[]string{"router_id", "peer"},
		),
		sentMessagesOpen: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "anycastd",
				Subsystem: "gobgp",
				Name:      "sent_messages_open",
				Help:      "Number of Open messages sent to the peer",
			},
			[]string{"router_id", "peer"},
		),
		sentMessagesKeepalive: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "anycastd",
				Subsystem: "gobgp",
				Name:      "sent_messages_keepalive",
				Help:      "Number of Keepalive messages sent to the peer",
			},
			[]string{"router_id", "peer"},
		),
		sentMessagesRefresh: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "anycastd",
				Subsystem: "gobgp",
				Name:      "sent_messages_refresh",
				Help:      "Number of Refresh messages sent to the peer",
			},
			[]string{"router_id", "peer"},
		),
		sentMessagesWithdrawUpdate: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "anycastd",
				Subsystem: "gobgp",
				Name:      "sent_messages_withdraw_update",
				Help:      "Number of Withdraw Update messages sent to the peer",
			},
			[]string{"router_id", "peer"},
		),
		sentMessagesWithdrawPrefix: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "anycastd",
				Subsystem: "gobgp",
				Name:      "sent_messages_withdraw_prefix",
				Help:      "Number of Withdraw Prefix messages sent to the peer",
			},
			[]string{"router_id", "peer"},
		),

		bgpPeerOutQueueCount: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "anycastd",
				Subsystem: "gobgp",
				Name:      "peer_out_queue_count",
				Help:      "Peer outgoing messages queue",
			},
			[]string{"router_id", "peer"},
		),
		bgpPeerFlopsCount: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "anycastd",
				Subsystem: "gobgp",
				Name:      "peer_flops_count",
				Help:      "Peer flops count",
			},
			[]string{"router_id", "peer"},
		),
		bgpPeerSendCommunityFlag: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "anycastd",
				Subsystem: "gobgp",
				Name:      "peer_send_community_flag",
				Help:      "Whether the peer have send community flag set",
			},
			[]string{"router_id", "peer"},
		),
		bgpPeerRemovePrivateFlag: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "anycastd",
				Subsystem: "gobgp",
				Name:      "peer_remove_private_flag",
				Help:      "Whether the peer have remove private flag set",
			},
			[]string{"router_id", "peer"},
		),
		bgpPeerPasswordSetFlag: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "anycastd",
				Subsystem: "gobgp",
				Name:      "peer_password_set_flag",
				Help:      "Whether the peer have peer password set flag set",
			},
			[]string{"router_id", "peer"},
		),
		bgpPeerType: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "anycastd",
				Subsystem: "gobgp",
				Name:      "peer_type",
				Help:      "Peer type 0=internal, 1=external",
			},
			[]string{"router_id", "peer"},
		),

		bgpSrv:   bgpSrv,
		routerID: routerID,
		asn:      asn,
	}
	return m
}

func (m *metrics) Run(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			peers := []*apipb.Peer{}
			if err := m.bgpSrv.ListPeer(ctx, &apipb.ListPeerRequest{}, func(p *apipb.Peer) {
				peers = append(peers, p)
			}); err != nil {
				return err
			}

			m.peerCount.WithLabelValues(m.routerID).Set(float64(len(peers)))

			for _, peer := range peers {
				peerState := peer.GetState()
				peerRouterID := peer.GetConf().GetNeighborAddress()

				m.peerAdminState.WithLabelValues(
					m.routerID,
					peerRouterID,
				).Set(float64(peerState.GetAdminState()))

				m.peerSessionState.WithLabelValues(
					m.routerID,
					peerRouterID,
				).Set(float64(peerState.GetSessionState()))

				m.bgpPeerOutQueueCount.WithLabelValues(
					m.routerID,
					peerRouterID,
				).Set(float64(peerState.GetOutQ()))

				m.bgpPeerFlopsCount.WithLabelValues(
					m.routerID,
					peerRouterID,
				).Set(float64(peerState.GetFlops()))

				m.bgpPeerSendCommunityFlag.WithLabelValues(
					m.routerID,
					peerRouterID,
				).Set(float64(peerState.GetSendCommunity()))

				m.bgpPeerRemovePrivateFlag.WithLabelValues(
					m.routerID,
					peerRouterID,
				).Set(float64(peerState.GetRemovePrivate()))

				passwordSetFlag := 0.0
				if peerState.GetAuthPassword() != "" {
					passwordSetFlag = 1.0
				}
				m.bgpPeerPasswordSetFlag.WithLabelValues(
					m.routerID,
					peerRouterID,
				).Set(passwordSetFlag)

				m.bgpPeerType.WithLabelValues(
					m.routerID,
					peerRouterID,
				).Set(float64(peerState.GetType()))

				if peerMessages := peerState.GetMessages(); peerMessages != nil {
					if msgs := peerMessages.GetReceived(); msgs != nil {
						m.receivedMessagesTotal.WithLabelValues(
							m.routerID,
							peerRouterID,
						).Set(float64(msgs.GetTotal()))

						m.receivedMessagesNotification.WithLabelValues(
							m.routerID,
							peerRouterID,
						).Set(float64(msgs.GetNotification()))

						m.receivedMessagesUpdate.WithLabelValues(
							m.routerID,
							peerRouterID,
						).Set(float64(msgs.GetUpdate()))

						m.receivedMessagesOpen.WithLabelValues(
							m.routerID,
							peerRouterID,
						).Set(float64(msgs.GetOpen()))

						m.receivedMessagesKeepalive.WithLabelValues(
							m.routerID,
							peerRouterID,
						).Set(float64(msgs.GetKeepalive()))

						m.receivedMessagesRefresh.WithLabelValues(
							m.routerID,
							peerRouterID,
						).Set(float64(msgs.GetRefresh()))

						m.receivedMessagesWithdrawUpdate.WithLabelValues(
							m.routerID,
							peerRouterID,
						).Set(float64(msgs.GetWithdrawUpdate()))

						m.receivedMessagesWithdrawUpdate.WithLabelValues(
							m.routerID,
							peerRouterID,
						).Set(float64(msgs.GetWithdrawPrefix()))
					}

					if msgs := peerMessages.GetSent(); msgs != nil {
						m.sentMessagesTotal.WithLabelValues(
							m.routerID,
							peerRouterID,
						).Set(float64(msgs.GetTotal()))

						m.sentMessagesNotification.WithLabelValues(
							m.routerID,
							peerRouterID,
						).Set(float64(msgs.GetNotification()))

						m.sentMessagesUpdate.WithLabelValues(
							m.routerID,
							peerRouterID,
						).Set(float64(msgs.GetUpdate()))

						m.sentMessagesOpen.WithLabelValues(
							m.routerID,
							peerRouterID,
						).Set(float64(msgs.GetOpen()))

						m.sentMessagesKeepalive.WithLabelValues(
							m.routerID,
							peerRouterID,
						).Set(float64(msgs.GetKeepalive()))

						m.sentMessagesRefresh.WithLabelValues(
							m.routerID,
							peerRouterID,
						).Set(float64(msgs.GetRefresh()))

						m.sentMessagesWithdrawUpdate.WithLabelValues(
							m.routerID,
							peerRouterID,
						).Set(float64(msgs.GetWithdrawUpdate()))

						m.sentMessagesWithdrawPrefix.WithLabelValues(
							m.routerID,
							peerRouterID,
						).Set(float64(msgs.GetWithdrawPrefix()))
					}
				}
			}

			time.Sleep(1 * time.Second)
		}
	}
}

func (m *metrics) Register() error {
	for _, mc := range []prometheus.Collector{
		m.peerCount,
		m.peerAdminState,
		m.peerSessionState,

		m.bgpPeerOutQueueCount,
		m.bgpPeerFlopsCount,
		m.bgpPeerSendCommunityFlag,
		m.bgpPeerRemovePrivateFlag,
		m.bgpPeerPasswordSetFlag,
		m.bgpPeerType,

		m.receivedMessagesTotal,
		m.receivedMessagesNotification,
		m.receivedMessagesUpdate,
		m.receivedMessagesOpen,
		m.receivedMessagesKeepalive,
		m.receivedMessagesRefresh,
		m.receivedMessagesWithdrawUpdate,
		m.receivedMessagesWithdrawPrefix,

		m.sentMessagesTotal,
		m.sentMessagesNotification,
		m.sentMessagesUpdate,
		m.sentMessagesOpen,
		m.sentMessagesKeepalive,
		m.sentMessagesRefresh,
		m.sentMessagesWithdrawUpdate,
		m.sentMessagesWithdrawPrefix,
	} {
		if err := prometheus.Register(mc); err != nil {
			return err
		}
	}

	return nil
}
