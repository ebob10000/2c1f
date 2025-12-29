package main

import (
	"fmt"
	"io"
	"log"
	"os"

	"github.com/ebob10000/2c1f/cmd"
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

	switch os.Args[1] {
	case "send":
		cmd.Send(os.Args[2:])
	case "receive":
		cmd.Receive(os.Args[2:])
	default:
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println("2C1F - Simple & Fast P2P File Transfer")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  2c1f send [flags] <folder/file>")
	fmt.Println("  2c1f receive [flags] <code>")
	fmt.Println()
	fmt.Println("Flags:")
	fmt.Println("  send:")
	fmt.Println("    -compress        Enable compression")
	fmt.Println("    -cache-manifest  Cache manifest file")
	fmt.Println("  receive:")
	fmt.Println("    -o <path>      Output directory")
}
