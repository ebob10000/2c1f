# 2c1f

Simple peer-to-peer file transfer tool. Share files between computers using a 6-digit code.

## Usage

### Download
Download the latest release for Windows, macOS, or Linux from the [Releases page](https://github.com/ebob10000/2c1f/releases/latest).

### Send
1. Open the application.
2. Select files or folders to send.
3. Share the generated 6-digit code with the receiver.

### Receive
1. Open the application.
2. Enter the 6-digit code provided by the sender.
3. The transfer will begin automatically.

## Build from Source

Requirements: Go 1.21+, Node.js 16+

Install Wails:
```bash
go install github.com/wailsapp/wails/v2/cmd/wails@latest
```

Build the project:
```bash
git clone https://github.com/ebob10000/2c1f.git
cd 2c1f
wails build
```

The binary will be located in `build/bin/`.

## License
Open source.