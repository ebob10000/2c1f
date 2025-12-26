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
		if len(os.Args) < 3 {
			fmt.Println("Usage: 2c1f send <folder>")
			os.Exit(1)
		}
		cmd.Send(os.Args[2])
	case "receive":
		if len(os.Args) < 3 {
			fmt.Println("Usage: 2c1f receive <code>")
			os.Exit(1)
		}
		cmd.Receive(os.Args[2])
	default:
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println("2C1F - Simple & Fast P2P File Transfer")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  2c1f send <folder>    Share a folder and get a connection code")
	fmt.Println("  2c1f receive <code>   Receive a folder using a connection code")
}
