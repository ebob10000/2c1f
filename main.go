package main

import (
	"fmt"
	"os"

	"github.com/2c1f/2c1f/cmd"
)

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
