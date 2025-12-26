# 2c1f - Two Computers, One File

**2c1f** is a blazing fast, secure, and simple P2P file transfer tool for the command line. It uses a 3-word code (like `apple-banana-cookie`) to connect two computers directly, even if they are behind different firewalls or NATs.

## Fast Setup

### Option 1: Install with Go (Recommended)
If you have Go installed, this is the easiest way. It compiles the app and adds it to your system path automatically.

```bash
go install github.com/2c1f/2c1f@latest
```
*(Note: Replace `github.com/2c1f/2c1f` with your actual repo URL once published)*

### Option 2: Build from Source
1. Clone the repo:
   ```bash
   git clone https://github.com/YOUR_USERNAME/2c1f.git
   cd 2c1f
   ```
2. Build it:
   ```bash
   go build -o 2c1f.exe main.go
   ```

## Usage

### Sending a Folder
1. Go to the directory containing the folder you want to send.
2. Run:
   ```bash
   2c1f send ./my-folder
   ```
3. Copy the generated code (e.g., `purple-hero-galaxy`).

### Receiving a Folder
1. On the receiving computer, run:
   ```bash
   2c1f receive purple-hero-galaxy
   ```
2. The transfer will start immediately.

## Features
- **Peer-to-Peer:** Direct transfer between devices.
- **NAT Traversal:** Works across different networks (home vs. office) using libp2p.
- **Local Discovery:** Instantly finds peers on the same Wi-Fi.
- **Resumable:** If the connection drops, just run the command again to resume.
- **Compressed:** Text/code transfers are compressed for speed.
- **Secure:** End-to-end encrypted streams.

## Tech Stack
- **Language:** Go (Golang)
- **Networking:** libp2p (DHT, Relay, MDNS)
- **Discovery:** Public DHT + Local MDNS
