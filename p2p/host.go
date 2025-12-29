package p2p

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/libp2p/go-libp2p/p2p/discovery/mdns"
	"github.com/libp2p/go-libp2p/p2p/discovery/routing"
	"github.com/libp2p/go-libp2p/p2p/security/noise"
	libp2ptls "github.com/libp2p/go-libp2p/p2p/security/tls"
	"github.com/multiformats/go-multiaddr"
)

const (
	ProtocolID      = "/2c1f/transfer/2.1.1"
	RendezvousNS    = "2c1f-rendezvous"
	DiscoveryPeriod = 10 * time.Second
	MDNSServiceTag  = "2c1f-local"
)

var BootstrapPeers = []string{
	"/dnsaddr/bootstrap.libp2p.io/p2p/QmNnooDu7bfjPFoTZYxMNLWUQJyrVwtbZg5gBMjTezGAJN",
	"/dnsaddr/bootstrap.libp2p.io/p2p/QmQCU2EcMqAqQPR2i9bChDtGNJchTbq5TbXJJ16u19uLTa",
	"/dnsaddr/bootstrap.libp2p.io/p2p/QmbLHAnMoJPWSCR5Zhtx6BHJX9KiKNN6tpvbUcqanj75Nb",
	"/dnsaddr/bootstrap.libp2p.io/p2p/QmcZf59bWwK5XFi76CZX8cbJ4BhTzzA3gU1ZjYZcYW3dwt",
}

type Node struct {
	Host          host.Host
	DHT           *dht.IpfsDHT
	Ctx           context.Context
	Cancel        context.CancelFunc
	Discovery     *routing.RoutingDiscovery
	ConnectedPeer peer.ID
	mu            sync.Mutex
}

func NewNode(ctx context.Context) (*Node, error) {
	ctx, cancel := context.WithCancel(ctx)

	h, err := libp2p.New(
		libp2p.ListenAddrStrings(
			"/ip4/0.0.0.0/tcp/0",
			"/ip4/0.0.0.0/udp/0/quic-v1",
		),
		libp2p.Security(libp2ptls.ID, libp2ptls.New),
		libp2p.Security(noise.ID, noise.New),
		libp2p.NATPortMap(),
		libp2p.EnableHolePunching(),
		libp2p.EnableRelay(),
	)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create host: %w", err)
	}

	kadDHT, err := dht.New(ctx, h, dht.Mode(dht.ModeClient))
	if err != nil {
		h.Close()
		cancel()
		return nil, fmt.Errorf("failed to create DHT: %w", err)
	}

	node := &Node{
		Host:   h,
		DHT:    kadDHT,
		Ctx:    ctx,
		Cancel: cancel,
	}

	if err := node.setupLocalDiscovery(); err != nil {
		fmt.Printf("Warning: Failed to setup MDNS: %v\n", err)
	}

	return node, nil
}

func (n *Node) setupLocalDiscovery() error {
	s := mdns.NewMdnsService(n.Host, MDNSServiceTag, n)
	return s.Start()
}

func (n *Node) HandlePeerFound(pi peer.AddrInfo) {
	if pi.ID == n.Host.ID() {
		return
	}
	n.Host.Connect(n.Ctx, pi)
}

func (n *Node) Bootstrap() error {
	if err := n.DHT.Bootstrap(n.Ctx); err != nil {
		return fmt.Errorf("failed to bootstrap DHT: %w", err)
	}

	var wg sync.WaitGroup
	connected := 0
	var connMu sync.Mutex

	for _, peerAddr := range BootstrapPeers {
		maddr, err := multiaddr.NewMultiaddr(peerAddr)
		if err != nil {
			continue
		}
		peerInfo, err := peer.AddrInfoFromP2pAddr(maddr)
		if err != nil {
			continue
		}

		wg.Add(1)
		go func(pi peer.AddrInfo) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(n.Ctx, 30*time.Second)
			defer cancel()
			if err := n.Host.Connect(ctx, pi); err == nil {
				connMu.Lock()
				connected++
				connMu.Unlock()
			}
		}(*peerInfo)
	}

	wg.Wait()

	if connected == 0 {
		return fmt.Errorf("failed to connect to any bootstrap peers")
	}

	n.Discovery = routing.NewRoutingDiscovery(n.DHT)

	return nil
}

func (n *Node) Advertise(code string) error {
	rendezvous := codeToRendezvous(code)

	_, err := n.Discovery.Advertise(n.Ctx, rendezvous)
	if err != nil {
		return fmt.Errorf("failed to advertise: %w", err)
	}

	return nil
}

func (n *Node) FindPeer(code string) (peer.ID, error) {
	rendezvous := codeToRendezvous(code)

	ctx, cancel := context.WithTimeout(n.Ctx, 30*time.Second)
	defer cancel()

	peerChan, err := n.Discovery.FindPeers(ctx, rendezvous)
	if err != nil {
		return "", fmt.Errorf("failed to find peers: %w", err)
	}

	for p := range peerChan {
		select {
		case <-n.Ctx.Done():
			return "", n.Ctx.Err()
		default:
		}

		if p.ID == n.Host.ID() {
			continue
		}
		if len(p.Addrs) == 0 {
			continue
		}

		ctxConn, cancelConn := context.WithTimeout(n.Ctx, 5*time.Second)
		err := n.Host.Connect(ctxConn, p)
		cancelConn()

		if err != nil {
			continue
		}

		n.mu.Lock()
		n.ConnectedPeer = p.ID
		n.mu.Unlock()
		return p.ID, nil
	}

	return "", fmt.Errorf("no peers found")
}

func (n *Node) SetStreamHandler(handler network.StreamHandler) {
	n.Host.SetStreamHandler(protocol.ID(ProtocolID), handler)
}

func (n *Node) NewStream(peerID peer.ID) (network.Stream, error) {
	return n.Host.NewStream(n.Ctx, peerID, protocol.ID(ProtocolID))
}

func (n *Node) Close() error {
	n.Cancel()
	if err := n.DHT.Close(); err != nil {
		return err
	}
	return n.Host.Close()
}

func codeToRendezvous(code string) string {
	hash := sha256.Sum256([]byte(code))
	return fmt.Sprintf("%s/%x", RendezvousNS, hash[:8])
}
