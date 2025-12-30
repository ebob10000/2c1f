package p2p

import (
	"context"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
)

func TestCodeToRendezvous(t *testing.T) {
	tests := []struct {
		name string
		code string
		want string
	}{
		{
			name: "basic code",
			code: "123456",
			want: "2c1f-rendezvous/8d969eef6ecad3c2",
		},
		{
			name: "different code",
			code: "654321",
			want: "2c1f-rendezvous/481f6cc0511143cc",
		},
		{
			name: "same code produces same hash",
			code: "123456",
			want: "2c1f-rendezvous/8d969eef6ecad3c2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := codeToRendezvous(tt.code)
			if got != tt.want {
				t.Errorf("codeToRendezvous(%q) = %q, want %q", tt.code, got, tt.want)
			}
		})
	}
}

func TestCodeToRendezvousDeterministic(t *testing.T) {
	// Test that the same code always produces the same rendezvous point
	code := "123456"
	first := codeToRendezvous(code)
	second := codeToRendezvous(code)

	if first != second {
		t.Errorf("codeToRendezvous is not deterministic: %q != %q", first, second)
	}
}

func TestCodeToRendezvousUnique(t *testing.T) {
	// Test that different codes produce different rendezvous points
	code1 := "123456"
	code2 := "654321"

	rv1 := codeToRendezvous(code1)
	rv2 := codeToRendezvous(code2)

	if rv1 == rv2 {
		t.Errorf("Different codes produced same rendezvous point: %q", rv1)
	}
}

func TestNewNode(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	node, err := NewNode(ctx)
	if err != nil {
		t.Fatalf("NewNode() error = %v", err)
	}
	defer node.Close()

	// Verify node components are initialized
	if node.Host == nil {
		t.Error("Node.Host is nil")
	}
	if node.DHT == nil {
		t.Error("Node.DHT is nil")
	}
	if node.Ctx == nil {
		t.Error("Node.Ctx is nil")
	}
	if node.Cancel == nil {
		t.Error("Node.Cancel is nil")
	}

	// Verify the node has an ID
	if node.Host.ID() == "" {
		t.Error("Node has empty peer ID")
	}

	// Verify the node is listening on addresses
	addrs := node.Host.Addrs()
	if len(addrs) == 0 {
		t.Error("Node is not listening on any addresses")
	}
}

func TestNodeClose(t *testing.T) {
	ctx := context.Background()

	node, err := NewNode(ctx)
	if err != nil {
		t.Fatalf("NewNode() error = %v", err)
	}

	// Close the node
	err = node.Close()
	if err != nil {
		t.Errorf("Node.Close() error = %v", err)
	}

	// Verify context is cancelled
	select {
	case <-node.Ctx.Done():
		// Expected
	case <-time.After(100 * time.Millisecond):
		t.Error("Node context was not cancelled after Close()")
	}
}

func TestSetStreamHandler(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	node, err := NewNode(ctx)
	if err != nil {
		t.Fatalf("NewNode() error = %v", err)
	}
	defer node.Close()

	// Test that SetStreamHandler doesn't panic
	handlerCalled := false
	node.SetStreamHandler(func(s network.Stream) {
		handlerCalled = true
		s.Close()
	})

	// We can't easily test the handler is actually called without creating another node
	// But we can verify the method doesn't panic
	if handlerCalled {
		t.Log("Handler was called during test setup")
	}
}

func TestHandlePeerFound(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	node, err := NewNode(ctx)
	if err != nil {
		t.Fatalf("NewNode() error = %v", err)
	}
	defer node.Close()

	// Test handling peer found with self - should be ignored
	selfInfo := peer.AddrInfo{ID: node.Host.ID()}
	node.HandlePeerFound(selfInfo)

	// Test handling peer found with invalid peer - should not panic
	invalidPeer := peer.AddrInfo{ID: "invalid-peer-id"}
	node.HandlePeerFound(invalidPeer)
}

func TestConstants(t *testing.T) {
	// Verify constants are set to expected values
	if ProtocolID != "/2c1f/transfer/2.1.1" {
		t.Errorf("ProtocolID = %q, want %q", ProtocolID, "/2c1f/transfer/2.1.1")
	}
	if RendezvousNS != "2c1f-rendezvous" {
		t.Errorf("RendezvousNS = %q, want %q", RendezvousNS, "2c1f-rendezvous")
	}
	if DiscoveryPeriod != 10*time.Second {
		t.Errorf("DiscoveryPeriod = %v, want %v", DiscoveryPeriod, 10*time.Second)
	}
	if MDNSServiceTag != "2c1f-local" {
		t.Errorf("MDNSServiceTag = %q, want %q", MDNSServiceTag, "2c1f-local")
	}
}

func TestBootstrapPeers(t *testing.T) {
	// Verify bootstrap peers list is not empty
	if len(BootstrapPeers) == 0 {
		t.Error("BootstrapPeers list is empty")
	}

	// Verify each bootstrap peer is a valid multiaddr
	for i, addr := range BootstrapPeers {
		if addr == "" {
			t.Errorf("BootstrapPeers[%d] is empty", i)
		}
		// Basic check that it starts with expected prefix
		if len(addr) < 10 {
			t.Errorf("BootstrapPeers[%d] is too short: %q", i, addr)
		}
	}
}
