package cmd

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/ebob10000/2c1f/p2p"
	"github.com/ebob10000/2c1f/transfer"
	"github.com/schollz/progressbar/v3"
)

func Receive(args []string) {
	fs := flag.NewFlagSet("receive", flag.ExitOnError)
	outputDir := fs.String("o", "", "Output directory")
	fastResume := fs.Bool("fast-resume", false, "Enable fast resume (skip hashing existing files)")
	fs.Parse(args)

	code := fs.Arg(0)
	if code == "" {
		fmt.Print("Enter connection code: ")
		fmt.Scanln(&code)
	}
	if code == "" {
		fmt.Println("Error: Code required")
		os.Exit(1)
	}

	destPath := *outputDir
	if destPath == "" {
		var err error
		destPath, err = os.Getwd()
		if err != nil {
			destPath = "."
		}
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
	defer stream.Close()

	receiver := transfer.NewReceiver(destPath)
	receiver.Code = code
	receiver.FastResume = *fastResume

	receiver.OnConfirmation = func(m *transfer.Manifest) bool {
		fmt.Println("\nIncoming Transfer:")
		fmt.Printf("  Name: %s\n", m.FolderName)
		fmt.Printf("  Size: %s\n", transfer.FormatBytes(m.TotalSize))
		fmt.Printf("  Files: %d\n", len(m.Files))

		var existingSize int64
		destFolder := filepath.Join(destPath, m.FolderName)
		for _, file := range m.Files {
			localPath := filepath.Join(destFolder, filepath.FromSlash(file.Path))
			info, err := os.Stat(localPath)
			if err == nil && !info.IsDir() {
				if info.Size() <= file.Size {
					existingSize += info.Size()
				}
			}
		}

		if existingSize > 0 {
			fmt.Printf("  Resuming: found %s existing data\n", transfer.FormatBytes(existingSize))
		}

		fmt.Print("Accept? [y/N]: ")
		var response string
		fmt.Scanln(&response)
		if response == "y" || response == "Y" {
			return true
		}
		fmt.Println("Transfer rejected.")
		return false
	}

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

	maxRetries := 5
	for attempt := 0; attempt <= maxRetries; attempt++ {
		err := receiver.Receive(stream)
		if err == nil {
			break
		}

		if transfer.IsRetryableError(err) && attempt < maxRetries {
			fmt.Printf("\nConnection interrupted: %v\n", err)
			fmt.Printf("Retrying (%d/%d)...\n", attempt+1, maxRetries)

			stream.Close()

			backoff := time.Duration(1<<attempt) * 2 * time.Second
			time.Sleep(backoff)

			fmt.Println("Reconnecting to sender...")
			newPeerID, findErr := node.FindPeer(code)
			if findErr != nil {
				fmt.Printf("Error: Failed to find peer: %v\n", findErr)
				os.Exit(1)
			}

			newStream, streamErr := node.NewStream(newPeerID)
			if streamErr != nil {
				fmt.Printf("Error: Failed to open stream: %v\n", streamErr)
				os.Exit(1)
			}
			stream = newStream

			if bar != nil {
				bar.Reset()
			}

			continue
		}

		fmt.Printf("Error: Transfer failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("\nFiles saved to: %s\n", filepath.Join(destPath, receiver.Manifest.FolderName))
}
