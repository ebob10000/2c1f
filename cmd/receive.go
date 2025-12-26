package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/ebob10000/2c1f/p2p"
	"github.com/ebob10000/2c1f/transfer"
)

// Receive starts the receiver mode
func Receive(code string) {
	// Get current directory as destination
	destPath, err := os.Getwd()
	if err != nil {
		destPath = "."
	}

	fmt.Printf("Code: %s\n", code)
	fmt.Printf("Destination: %s\n", destPath)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		fmt.Println("\nShutting down...")
		cancel()
	}()

	fmt.Println("Starting P2P node...")
	node, err := p2p.NewNode(ctx)
	if err != nil {
		fmt.Printf("Error: Failed to create P2P node: %v\n", err)
		os.Exit(1)
	}
	defer node.Close()

	fmt.Printf("Node ID: %s\n", node.Host.ID().String()[:12])

	fmt.Println("Connecting to network...")
	if err := node.Bootstrap(); err != nil {
		fmt.Printf("Error: Failed to bootstrap: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Searching for sender...")
	peerID, err := node.FindPeer(code)
	if err != nil {
		fmt.Printf("Error: Failed to find peer: %v\n", err)
		os.Exit(1)
	}

	stream, err := node.NewStream(peerID)
	if err != nil {
		fmt.Printf("Error: Failed to open stream: %v\n", err)
		os.Exit(1)
	}
	
	compressedStream, err := transfer.NewCompressedStream(stream)
	if err != nil {
		fmt.Printf("Error: Failed to initialize compression: %v\n", err)
		stream.Close()
		os.Exit(1)
	}
	defer compressedStream.Close()

	receiver := transfer.NewReceiver(destPath)
	if err := receiver.Receive(compressedStream); err != nil {
		fmt.Printf("Error: Transfer failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("\nFiles saved to: %s\n", filepath.Join(destPath, receiver.Manifest.FolderName))
}
