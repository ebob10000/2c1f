package cmd

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ebob10000/2c1f/p2p"
	"github.com/ebob10000/2c1f/transfer"
	"github.com/ebob10000/2c1f/words"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/schollz/progressbar/v3"
)

func Send(args []string) {
	fs := flag.NewFlagSet("send", flag.ExitOnError)
	compress := fs.Bool("compress", false, "Enable compression")
	cacheManifest := fs.Bool("cache-manifest", false, "Cache manifest file")
	skipHash := fs.Bool("skip-hash", false, "Skip file hashing (faster start, less secure resume)")
	fs.Parse(args)

	folderPath := fs.Arg(0)
	if folderPath == "" {
		fmt.Print("Enter path to file or folder: ")
		fmt.Scanln(&folderPath)
	}
	if folderPath == "" {
		fmt.Println("Error: Path required")
		os.Exit(1)
	}

	_, err := os.Stat(folderPath)
	if err != nil {
		fmt.Printf("Error: Cannot access path: %v\n", err)
		os.Exit(1)
	}

	sender, err := transfer.NewSender(folderPath, *cacheManifest, *skipHash, func(path string, size int64) {
		fmt.Printf("\rHashing: %s...", path)
	})
	if err != nil {
		fmt.Printf("\nError: Failed to scan path: %v\n", err)
		os.Exit(1)
	}
	fmt.Println()
	sender.Compress = *compress

	fmt.Printf("Sending: %s (%d files)\n", sender.Manifest.FolderName, len(sender.Manifest.Files))

	fileOffsets := make(map[string]int64)
	var currentOffset int64
	for _, f := range sender.Manifest.Files {
		fileOffsets[f.Path] = currentOffset
		currentOffset += f.Size
	}

	bar := progressbar.NewOptions64(
		sender.Manifest.TotalSize,
		progressbar.OptionSetDescription("sending"),
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

	sender.OnStartFile = func(filename string, index, total int) {
		bar.Describe(fmt.Sprintf("Sending %s (%d/%d)", filename, index, total))
	}

	sender.OnProgress = func(filename string, sent, total int64) {
		if offset, ok := fileOffsets[filename]; ok {
			bar.Set64(offset + sent)
		}
	}

	code, err := words.Generate()
	if err != nil {
		fmt.Printf("Error: Failed to generate code: %v\n", err)
		os.Exit(1)
	}
	sender.Code = code

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
	var peerAccepted bool

	node.SetStreamHandler(func(stream network.Stream) {
		peerID := stream.Conn().RemotePeer()
		fmt.Printf("\nPeer connected: %s\n", peerID.String()[:12])

		err := sender.Handshake(stream)
		if err != nil {
			fmt.Printf("Handshake failed: %v\n", err)
			stream.Close()
			return
		}

		if !peerAccepted {
			fmt.Printf("Connection request from %s. Accept? [y/N]: ", peerID.String()[:12])
			var response string
			fmt.Scanln(&response)
			if response != "y" && response != "Y" {
				fmt.Println("Connection rejected.")
				stream.Close()
				return
			}
			peerAccepted = true
		} else {
			fmt.Println("Receiver reconnected, resuming transfer...")
		}

		var dataStream io.ReadWriter = stream
		if sender.Compress {
			compressedStream, err := transfer.NewCompressedStream(stream)
			if err != nil {
				fmt.Printf("Failed to initialize compression: %v\n", err)
				stream.Close()
				if transfer.IsRetryableError(err) {
					fmt.Println("Waiting for receiver to reconnect...")
					return
				}
				transferDone <- err
				return
			}
			defer compressedStream.Close()
			dataStream = compressedStream
		}

		err = sender.Send(dataStream)
		if err != nil {
			if transfer.IsRetryableError(err) {
				fmt.Printf("\nConnection interrupted: %v\n", err)
				fmt.Println("Waiting for receiver to reconnect...")
				stream.Close()
				return
			}
		}
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
		fmt.Println("Transfer complete!")
	case <-ctx.Done():
		fmt.Println("Cancelled.")
	}
}
