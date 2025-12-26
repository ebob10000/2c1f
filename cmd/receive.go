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
	"github.com/schollz/progressbar/v3"
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

	var bar *progressbar.ProgressBar
	fileOffsets := make(map[string]int64)

	receiver.OnStartFile = func(filename string, index, total int) {
		if bar == nil {
			if receiver.Manifest != nil {
				var currentOffset int64
				for _, f := range receiver.Manifest.Files {
					fileOffsets[f.Path] = currentOffset
					currentOffset += f.Size
				}
				bar = progressbar.NewOptions64(
					receiver.Manifest.TotalSize,
					progressbar.OptionSetDescription("receiving"),
					progressbar.OptionShowBytes(true),
					progressbar.OptionSetWidth(20),
					progressbar.OptionShowCount(),
					progressbar.OptionOnCompletion(func() {
						fmt.Println()
					}),
					progressbar.OptionSetTheme(progressbar.Theme{
						Saucer:        "=",
						SaucerHead:    ">",
						SaucerPadding: " ",
						BarStart:      "[",
						BarEnd:        "]",
					}),
				)
			}
		}
		if bar != nil {
			bar.Describe(fmt.Sprintf("Receiving %s (%d/%d)", filename, index, total))
		}
	}

	receiver.OnProgress = func(filename string, received, total int64) {
		if bar != nil {
			if offset, ok := fileOffsets[filename]; ok {
				bar.Set64(offset + received)
			}
		}
	}

	if err := receiver.Receive(compressedStream); err != nil {
		fmt.Printf("Error: Transfer failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("\nFiles saved to: %s\n", filepath.Join(destPath, receiver.Manifest.FolderName))
}
