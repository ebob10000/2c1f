package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/ebob10000/2c1f/cmd"
	"github.com/ebob10000/2c1f/settings"
	golog "github.com/ipfs/go-log/v2"
)

func init() {
	// Disable standard log output (used by some libraries like zeroconf)
	log.SetOutput(io.Discard)

	// Set ipfs/go-log to error level to suppress warnings from libp2p/mdns
	golog.SetAllLoggers(golog.LevelError)
}

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	firstArg := os.Args[1]

	// Handle receive command
	if firstArg == "receive" {
		// Edge case: check if "receive" is actually a file/folder in current directory
		if len(os.Args) == 2 {
			// No code provided, check if "receive" exists as a file/folder
			if _, err := os.Stat("receive"); err == nil {
				// File/folder named "receive" exists, treat as send path
				handleSend("receive", os.Args[2:])
				return
			}
		}
		// Normal receive command
		cmd.Receive(os.Args[2:])
		return
	}

	// Otherwise treat as path for sending
	handleSend(firstArg, os.Args[2:])
}

func handleSend(path string, args []string) {
	// Validate path exists
	if _, err := os.Stat(path); err != nil {
		fmt.Fprintf(os.Stderr, "Error: Cannot access path '%s': %v\n", path, err)
		os.Exit(1)
	}

	// Load settings from file
	userSettings := settings.LoadSettings()

	// Parse optional flags (override defaults from settings)
	fs := flag.NewFlagSet("send", flag.ExitOnError)
	compress := fs.Bool("compress", userSettings.Compress, "Enable compression")
	cacheManifest := fs.Bool("cache-manifest", userSettings.CacheManifest, "Cache manifest file")
	skipHash := fs.Bool("skip-hash", !userSettings.AutoHash, "Skip file hashing")
	fs.Parse(args)

	// Construct args array for cmd.Send
	var sendArgs []string
	if *compress {
		sendArgs = append(sendArgs, "-compress")
	}
	if *cacheManifest {
		sendArgs = append(sendArgs, "-cache-manifest")
	}
	if *skipHash {
		sendArgs = append(sendArgs, "-skip-hash")
	}
	sendArgs = append(sendArgs, path)

	cmd.Send(sendArgs)
}

func printUsage() {
	fmt.Println("2C1F - Simple & Fast P2P File Transfer")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  2c1f <folder/file> [flags]")
	fmt.Println("  2c1f receive <code> [flags]")
	fmt.Println()
	fmt.Println("Flags:")
	fmt.Println("  -compress        Enable compression")
	fmt.Println("  -cache-manifest  Cache manifest file")
	fmt.Println("  -skip-hash       Skip file hashing")
	fmt.Println()
	fmt.Println("  receive:")
	fmt.Println("    -o <path>        Output directory")
	fmt.Println("    -fast-resume     Fast resume (skip hashing)")
}
