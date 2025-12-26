package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ebob10000/2c1f/p2p"
	"github.com/ebob10000/2c1f/transfer"
	"github.com/ebob10000/2c1f/words"
	"github.com/libp2p/go-libp2p/core/network"
)

// Send starts the sender mode
func Send(folderPath string) {
	info, err := os.Stat(folderPath)
	if err != nil {
		fmt.Printf("Error: Cannot access folder: %v\n", err)
		os.Exit(1)
	}
	if !info.IsDir() {
		fmt.Printf("Error: Path is not a directory: %s\n", folderPath)
		os.Exit(1)
	}

	sender, err := transfer.NewSender(folderPath)
	if err != nil {
		fmt.Printf("Error: Failed to scan folder: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Folder: %s (%d files)\n", sender.Manifest.FolderName, len(sender.Manifest.Files))

	code, err := words.Generate()
	if err != nil {
		fmt.Printf("Error: Failed to generate code: %v\n", err)
		os.Exit(1)
	}

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

	time.Sleep(2 * time.Second)

	if err := node.Advertise(code); err != nil {
		fmt.Printf("Error: Failed to advertise: %v\n", err)
		os.Exit(1)
	}

	transferDone := make(chan error, 1)
	node.SetStreamHandler(func(stream network.Stream) {
		fmt.Printf("\nPeer connected: %s\n", stream.Conn().RemotePeer().String()[:12])

		compressedStream, err := transfer.NewCompressedStream(stream)
		if err != nil {
			fmt.Printf("Failed to initialize compression: %v\n", err)
			stream.Close()
			transferDone <- err
			return
		}
		defer compressedStream.Close()

		err = sender.Send(compressedStream)
		transferDone <- err
	})

	fmt.Println()
	fmt.Println("========================================")
	fmt.Printf("  CONNECTION CODE: %s\n", code)
	fmt.Println("========================================")
	fmt.Println()
	fmt.Println("Share this code with the receiver.")
	fmt.Println("Waiting for peer to connect...")

	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				node.Advertise(code)
			}
		}
	}()

	select {
	case err := <-transferDone:
		if err != nil {
			fmt.Printf("Transfer failed: %v\n", err)
			os.Exit(1)
		}
	case <-ctx.Done():
		fmt.Println("Cancelled.")
	}
}
