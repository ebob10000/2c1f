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
	ProtocolID      = "/2c1f/transfer/1.3.0"
	RendezvousNS    = "2c1f-rendezvous"
	DiscoveryPeriod = 10 * time.Second
	MDNSServiceTag  = "2c1f-local"
)

// Public IPFS bootstrap nodes
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

// NewNode creates a new libp2p node
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

// setupLocalDiscovery starts MDNS discovery
func (n *Node) setupLocalDiscovery() error {
	s := mdns.NewMdnsService(n.Host, MDNSServiceTag, n)
	return s.Start()
}

// HandlePeerFound implements mdns.Notifee
func (n *Node) HandlePeerFound(pi peer.AddrInfo) {
	if pi.ID == n.Host.ID() {
		return
	}
	// Attempt to connect to local peer
	n.Host.Connect(n.Ctx, pi)
}

// Bootstrap connects to bootstrap peers and initializes DHT
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

// Advertise announces this node on the DHT for a given rendezvous code
func (n *Node) Advertise(code string) error {
	rendezvous := codeToRendezvous(code)

	_, err := n.Discovery.Advertise(n.Ctx, rendezvous)
	if err != nil {
		return fmt.Errorf("failed to advertise: %w", err)
	}

	return nil
}

// FindPeer searches for a peer advertising the given code
func (n *Node) FindPeer(code string) (peer.ID, error) {
	rendezvous := codeToRendezvous(code)

	peerChan, err := n.Discovery.FindPeers(n.Ctx, rendezvous)
	if err != nil {
		return "", fmt.Errorf("failed to find peers: %w", err)
	}

	for p := range peerChan {
		if p.ID == n.Host.ID() {
			continue
		}
		if len(p.Addrs) == 0 {
			continue
		}

		ctx, cancel := context.WithTimeout(n.Ctx, 60*time.Second)
		err := n.Host.Connect(ctx, p)
		cancel()

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

// SetStreamHandler sets the handler for incoming streams
func (n *Node) SetStreamHandler(handler network.StreamHandler) {
	n.Host.SetStreamHandler(protocol.ID(ProtocolID), handler)
}

// NewStream opens a new stream to a peer
func (n *Node) NewStream(peerID peer.ID) (network.Stream, error) {
	return n.Host.NewStream(n.Ctx, peerID, protocol.ID(ProtocolID))
}

// Close shuts down the node
func (n *Node) Close() error {
	n.Cancel()
	if err := n.DHT.Close(); err != nil {
		return err
	}
	return n.Host.Close()
}

// codeToRendezvous converts a word code to a rendezvous string
func codeToRendezvous(code string) string {
	hash := sha256.Sum256([]byte(code))
	return fmt.Sprintf("%s/%x", RendezvousNS, hash[:8])
}
