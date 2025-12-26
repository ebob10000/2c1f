# 2c1f - Two Computers, One File

**2c1f** is a blazing fast, secure, and simple P2P file transfer tool for the command line. It uses a simple 6-digit code (like `123-456`) to connect two computers directly, even if they are behind different firewalls or NATs.

## Fast Setup

### Option 1: Install with Go (Recommended)
If you have Go installed, this is the easiest way. It compiles the app and adds it to your system path automatically.

```bash
go install github.com/ebob10000/2c1f@latest
```

### Option 2: Build from Source
1. Clone the repo:
   ```bash
   git clone https://github.com/ebob10000/2c1f.git
   cd 2c1f
   ```
2. Build it:
   ```bash
   go build -o 2c1f.exe main.go
   ```

## Usage

### Sending
You can send a single file or an entire folder.

**Send a folder:**
```bash
2c1f send ./my-folder
```

**Send a file (no compression):**
```bash
2c1f send -no-compress my-movie.mp4
```

Copy the generated code (e.g., `123-456`).

### Receiving
1. On the receiving computer, run:
   ```bash
   2c1f receive 123-456
   ```
2. You will be asked to confirm the file details before downloading.

**Receive to a specific location:**
```bash
2c1f receive -o D:\Downloads 123-456
```

### Interactive Mode
If you run commands without arguments, 2c1f will prompt you:

```bash
2c1f send
# Enter path to file or folder: ...
```

```bash
2c1f receive
# Enter connection code: ...
```

## Features
- **Peer-to-Peer:** Direct transfer between devices.
- **NAT Traversal:** Works across different networks (home vs. office) using libp2p.
- **Secure:** 
    - End-to-end encrypted streams.
    - **Authentication:** Receiver must provide the correct code.
    - **Confirmation:** Both Sender and Receiver must accept the connection/transfer.
- **Local Discovery:** Instantly finds peers on the same Wi-Fi.
- **Resumable:** If the connection drops, just run the command again to resume.
- **Single File Support:** Send individual files easily.
- **Smart Options:** Disable compression for large media files.

## Tech Stack
- **Language:** Go (Golang)
- **Networking:** libp2p (DHT, Relay, MDNS)
- **Discovery:** Public DHT + Local MDNS
