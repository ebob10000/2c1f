package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	goruntime "runtime"
	"sync"
	"time"

	"github.com/ebob10000/2c1f/p2p"
	"github.com/ebob10000/2c1f/settings"
	"github.com/ebob10000/2c1f/transfer"
	"github.com/ebob10000/2c1f/updater"
	"github.com/ebob10000/2c1f/version"
	"github.com/ebob10000/2c1f/words"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// TransferRecord stores info about a completed transfer
type TransferRecord struct {
	Timestamp time.Time `json:"timestamp"`
	Path      string    `json:"path"`
	FullPath  string    `json:"fullPath"`
	Size      int64     `json:"size"`
	Direction string    `json:"direction"`
	Status    string    `json:"status"`
}

type App struct {
	ctx             context.Context
	settings        settings.AppSettings
	activeNode      *p2p.Node
	nodeMu          sync.Mutex
	transferHistory []TransferRecord
	isPaused        bool
	pauseMu         sync.Mutex
}

// progressTracker handles progress tracking for transfers
type progressTracker struct {
	ctx          context.Context
	globalSent   int64
	globalTotal  int64
	lastUpdate   time.Time
	fileProgress map[string]int64
	mu           sync.Mutex
}

func newProgressTracker(ctx context.Context, totalSize int64) *progressTracker {
	return &progressTracker{
		ctx:          ctx,
		globalTotal:  totalSize,
		fileProgress: make(map[string]int64),
	}
}

func (pt *progressTracker) onStartFile(filename string, index, total int) {
	runtime.EventsEmit(pt.ctx, "transfer_start_file", map[string]interface{}{
		"filename": filename,
		"index":    index,
		"total":    total,
	})
}

func (pt *progressTracker) onProgress(filename string, sent, total int64) {
	pt.mu.Lock()
	defer pt.mu.Unlock()

	prevSent := pt.fileProgress[filename]
	delta := sent - prevSent
	pt.fileProgress[filename] = sent
	pt.globalSent += delta

	now := time.Now()
	if sent == total || now.Sub(pt.lastUpdate) > 500*time.Millisecond {
		runtime.EventsEmit(pt.ctx, "transfer_file_progress", map[string]interface{}{
			"filename": filename,
			"sent":     sent,
			"total":    total,
			"percent":  float64(sent) / float64(total) * 100,
		})
		runtime.EventsEmit(pt.ctx, "transfer_global_progress", map[string]interface{}{
			"sent":    pt.globalSent,
			"total":   pt.globalTotal,
			"percent": float64(pt.globalSent) / float64(pt.globalTotal) * 100,
		})
		pt.lastUpdate = now
	}
}

// simulateFileTransfer simulates transferring files with progress updates
// Returns true if transfer completed, false if cancelled
func (a *App) simulateFileTransfer(files []transfer.FileEntry, totalSize int64, direction string, checkCancel bool) bool {
	var globalSent int64 = 0
	for i, file := range files {
		runtime.EventsEmit(a.ctx, "transfer_start_file", map[string]interface{}{
			"filename": file.Path,
			"index":    i + 1,
			"total":    len(files),
		})

		chunkSize := int64(1024 * 1024 * 5) // 5MB chunks
		var sent int64 = 0
		for sent < file.Size {
			if a.IsPaused() {
				time.Sleep(500 * time.Millisecond)
				continue
			}

			// Check for cancellation if requested
			if checkCancel {
				a.nodeMu.Lock()
				cancelled := (a.activeNode == nil)
				a.nodeMu.Unlock()
				if cancelled {
					return false
				}
			}

			remaining := file.Size - sent
			if remaining < chunkSize {
				chunkSize = remaining
			}
			sent += chunkSize
			globalSent += chunkSize
			time.Sleep(50 * time.Millisecond) // Simulate network delay

			runtime.EventsEmit(a.ctx, "transfer_file_progress", map[string]interface{}{
				"filename": file.Path,
				"sent":     sent,
				"total":    file.Size,
				"percent":  float64(sent) / float64(file.Size) * 100,
			})

			runtime.EventsEmit(a.ctx, "transfer_global_progress", map[string]interface{}{
				"sent":    globalSent,
				"total":   totalSize,
				"percent": float64(globalSent) / float64(totalSize) * 100,
			})
		}
	}

	statusMsg := fmt.Sprintf("%s successfully (Simulation)", map[bool]string{true: "Sent", false: "Received"}[direction == "send"])
	runtime.EventsEmit(a.ctx, "transfer_complete", statusMsg)
	a.AddTransferRecord("Simulation Transfer", totalSize, direction, "complete")
	return true
}

func (a *App) loadSettings() {
	a.settings = settings.LoadSettings()
}

func (a *App) GetSettings() settings.AppSettings {
	return a.settings
}

func (a *App) SaveSettings(s settings.AppSettings) {
	a.settings = s
	path := settings.GetSettingsPath()
	data, err := json.Marshal(s)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to marshal settings: %v\n", err)
		return
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to save settings: %v\n", err)
	}
}

// NewApp creates a new App application struct
func NewApp() *App {
	a := &App{}
	a.loadSettings()
	a.loadHistory()
	return a
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	// Check for updates in background (non-blocking)
	go func() {
		// Wait a bit before checking to not slow down app startup
		time.Sleep(2 * time.Second)

		updateInfo, err := updater.CheckForUpdates("ebob10000/2c1f", version.Version)
		if err != nil {
			// Log error but don't notify user (fail silently)
			return
		}
		if updateInfo != nil {
			runtime.EventsEmit(a.ctx, "update_available", updateInfo)
		}
	}()
}

func (a *App) CancelTransfer() {
	a.nodeMu.Lock()
	node := a.activeNode
	a.activeNode = nil
	a.nodeMu.Unlock()

	if node != nil {
		node.Close()
	}
}

func (a *App) CopyToClipboard(text string) error {
	return runtime.ClipboardSetText(a.ctx, text)
}

// GetVersion returns the current application version
func (a *App) GetVersion() string {
	return version.Version
}

// DownloadAndInstallUpdate downloads and installs a new version
func (a *App) DownloadAndInstallUpdate(releaseVersion string) error {
	// Fetch release info
	release, err := updater.FetchLatestRelease("ebob10000/2c1f")
	if err != nil {
		runtime.EventsEmit(a.ctx, "update_error", map[string]string{"error": err.Error()})
		return err
	}

	// Find correct asset for platform
	asset, err := updater.GetAssetForPlatform(release, goruntime.GOOS, goruntime.GOARCH)
	if err != nil {
		runtime.EventsEmit(a.ctx, "update_error", map[string]string{"error": err.Error()})
		return err
	}

	// Download with progress callback
	tempPath, err := updater.DownloadUpdate(asset, func(downloaded, total int64) {
		percent := float64(downloaded) / float64(total) * 100
		runtime.EventsEmit(a.ctx, "update_download_progress", map[string]interface{}{
			"downloaded": downloaded,
			"total":      total,
			"percent":    percent,
		})
	})
	if err != nil {
		runtime.EventsEmit(a.ctx, "update_error", map[string]string{"error": err.Error()})
		return err
	}

	// Notify that download is complete and ready to install
	runtime.EventsEmit(a.ctx, "update_ready", map[string]string{"version": releaseVersion})

	// Replace and restart
	exePath, err := os.Executable()
	if err != nil {
		runtime.EventsEmit(a.ctx, "update_error", map[string]string{"error": fmt.Sprintf("Failed to get executable path: %v", err)})
		return err
	}
	if err := updater.ReplaceAndRestart(tempPath, exePath); err != nil {
		runtime.EventsEmit(a.ctx, "update_error", map[string]string{"error": err.Error()})
		return err
	}

	return nil
}

func (a *App) getHistoryPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		// Fallback to current directory if home dir can't be determined
		return ".2c1f-history.json"
	}
	return filepath.Join(home, ".2c1f-history.json")
}

func (a *App) loadHistory() {
	path := a.getHistoryPath()
	data, err := os.ReadFile(path)
	if err != nil {
		// File doesn't exist or can't be read - start with empty history
		return
	}
	if err := json.Unmarshal(data, &a.transferHistory); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to parse history file, starting fresh: %v\n", err)
		a.transferHistory = []TransferRecord{}
	}
}

func (a *App) saveHistory() {
	path := a.getHistoryPath()
	data, err := json.Marshal(a.transferHistory)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to marshal history: %v\n", err)
		return
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to save history: %v\n", err)
	}
}

func (a *App) GetTransferHistory() []TransferRecord {
	return a.transferHistory
}

func (a *App) AddTransferRecord(path string, size int64, direction, status string) {
	record := TransferRecord{
		Timestamp: time.Now(),
		Path:      filepath.Base(path),
		FullPath:  path,
		Size:      size,
		Direction: direction,
		Status:    status,
	}
	a.transferHistory = append([]TransferRecord{record}, a.transferHistory...)
	if len(a.transferHistory) > 50 {
		a.transferHistory = a.transferHistory[:50]
	}
	a.saveHistory()
}

func (a *App) ClearHistory() {
	a.transferHistory = []TransferRecord{}
	a.saveHistory()
}

func (a *App) IsPaused() bool {
	a.pauseMu.Lock()
	defer a.pauseMu.Unlock()
	return a.isPaused
}

func (a *App) SelectFile() (string, error) {
	return runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Select File to Send",
	})
}

func (a *App) SelectFolder() (string, error) {
	return runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Select Folder to Send",
	})
}

func (a *App) SelectSaveDirectory() (string, error) {
	return runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Select Destination Folder",
	})
}

func (a *App) StartSender(path string, compress bool, skipHash bool, cacheManifest bool) (string, error) {
	if isDevMode() {
		return a.startSimulatedSender(path)
	}

	go func() {
		runtime.EventsEmit(a.ctx, "sender_status", "Initializing...")

		onHashProgress := func(path string, size int64) {
			runtime.EventsEmit(a.ctx, "hashing_progress", map[string]interface{}{
				"filename": path,
				"size":     size,
			})
		}

		sender, err := transfer.NewSender(path, cacheManifest, skipHash, onHashProgress)
		if err != nil {
			runtime.EventsEmit(a.ctx, "error", fmt.Sprintf("Failed to prepare files: %v", err))
			return
		}
		sender.Compress = compress

		runtime.EventsEmit(a.ctx, "transfer_manifest", map[string]interface{}{
			"folderName": sender.Manifest.FolderName,
			"files":      sender.Manifest.Files,
			"totalSize":  sender.Manifest.TotalSize,
		})

		code, err := words.Generate()
		if err != nil {
			runtime.EventsEmit(a.ctx, "error", fmt.Sprintf("Failed to generate code: %v", err))
			return
		}
		sender.Code = code

		runtime.EventsEmit(a.ctx, "sender_ready", code)

		// Setup progress tracking
		progress := newProgressTracker(a.ctx, sender.Manifest.TotalSize)
		sender.OnStartFile = progress.onStartFile
		sender.OnProgress = progress.onProgress

		runtime.EventsEmit(a.ctx, "sender_status", "Starting P2P node...")

		node, err := p2p.NewNode(a.ctx)
		if err != nil {
			runtime.EventsEmit(a.ctx, "error", fmt.Sprintf("Failed to start p2p node: %v", err))
			return
		}

		a.nodeMu.Lock()
		a.activeNode = node
		a.nodeMu.Unlock()

		go func() {
			runtime.EventsEmit(a.ctx, "log", "Bootstrapping network...")
			if err := node.Bootstrap(); err != nil {
				runtime.EventsEmit(a.ctx, "error", fmt.Sprintf("Bootstrap failed: %v", err))
				return
			}
			runtime.EventsEmit(a.ctx, "log", "Network ready. Advertising code...")

			ticker := time.NewTicker(30 * time.Second)
			defer ticker.Stop()
			node.Advertise(code)

			for {
				select {
				case <-node.Ctx.Done():
					return
				case <-ticker.C:
					node.Advertise(code)
				}
			}
		}()

		runtime.EventsEmit(a.ctx, "sender_status", "Waiting for connection...")

		node.SetStreamHandler(func(stream network.Stream) {
			defer stream.Close()
			defer func() {
				a.nodeMu.Lock()
				cleanupNode := a.activeNode
				a.activeNode = nil
				a.nodeMu.Unlock()

				if cleanupNode != nil {
					cleanupNode.Close()
				}
			}()

			peerID := stream.Conn().RemotePeer()
			runtime.EventsEmit(a.ctx, "log", fmt.Sprintf("Peer connected: %s", peerID.String()[:12]))

			err := sender.Handshake(stream)
			if err != nil {
				runtime.EventsEmit(a.ctx, "error", fmt.Sprintf("Handshake failed: %v", err))
				return
			}

			if sender.Compress {
				compressed, err := transfer.NewCompressedStream(stream)
				if err != nil {
					runtime.EventsEmit(a.ctx, "error", fmt.Sprintf("Compression init failed: %v", err))
					return
				}
				defer compressed.Close()
				if err := sender.Send(compressed); err != nil {
					runtime.EventsEmit(a.ctx, "error", fmt.Sprintf("Transfer failed: %v", err))
					return
				}
			} else {
				if err := sender.Send(stream); err != nil {
					runtime.EventsEmit(a.ctx, "error", fmt.Sprintf("Transfer failed: %v", err))
					return
				}
			}

			runtime.EventsEmit(a.ctx, "transfer_complete", "Sent successfully")
			a.AddTransferRecord(path, sender.Manifest.TotalSize, "send", "complete")
		})
	}()

	return "", nil
}

func (a *App) StartReceiver(code, destPath string, fastResume bool) error {
	if isDevMode() {
		return a.startSimulatedReceiver(code, destPath)
	}
	receiver := transfer.NewReceiver(destPath)
	receiver.Code = code
	receiver.FastResume = fastResume

	// Progress will be initialized after manifest is received
	var progress *progressTracker

	receiver.OnConfirmation = func(m *transfer.Manifest) bool {
		// Initialize progress tracking with manifest total size
		progress = newProgressTracker(a.ctx, m.TotalSize)
		receiver.OnStartFile = progress.onStartFile
		receiver.OnProgress = progress.onProgress
		runtime.EventsEmit(a.ctx, "transfer_manifest", map[string]interface{}{
			"folderName": m.FolderName,
			"totalSize":  m.TotalSize,
			"fileCount":  len(m.Files),
			"files":      m.Files,
		})
		return true
	}

	go func() {
		node, err := p2p.NewNode(a.ctx)
		if err != nil {
			runtime.EventsEmit(a.ctx, "error", fmt.Sprintf("Failed to start node: %v", err))
			return
		}
		defer node.Close()

		runtime.EventsEmit(a.ctx, "log", "Bootstrapping...")
		if err := node.Bootstrap(); err != nil {
			runtime.EventsEmit(a.ctx, "error", fmt.Sprintf("Bootstrap failed: %v", err))
			return
		}

		runtime.EventsEmit(a.ctx, "log", "Finding peer...")

		var peerID peer.ID
		for i := 0; i < 60; i++ {
			p, err := node.FindPeer(code)
			if err == nil {
				peerID = p
				break
			}
			if i < 59 {
				if i%2 == 0 {
					runtime.EventsEmit(a.ctx, "log", fmt.Sprintf("Searching for sender... (%ds)", (i+1)/2))
				}
				time.Sleep(500 * time.Millisecond)
			}
		}

		if peerID == "" {
			runtime.EventsEmit(a.ctx, "error", "Peer not found. Make sure the sender is online and the code is correct.")
			return
		}

		runtime.EventsEmit(a.ctx, "log", "Connecting...")

		maxRetries := 5
		var lastErr error

		for attempt := 0; attempt <= maxRetries; attempt++ {
			if attempt > 0 {
				runtime.EventsEmit(a.ctx, "log", fmt.Sprintf("Retrying transfer (attempt %d/%d)...", attempt, maxRetries))
				p, err := node.FindPeer(code)
				if err != nil {
					lastErr = fmt.Errorf("failed to find peer during retry: %w", err)
					time.Sleep(2 * time.Second)
					continue
				}
				peerID = p
			}

			stream, err := node.NewStream(peerID)
			if err != nil {
				lastErr = fmt.Errorf("connection failed: %w", err)
				if attempt < maxRetries {
					time.Sleep(2 * time.Second)
					continue
				}
				break
			}

			err = receiver.Receive(stream)
			stream.Close()

			if err == nil {
				runtime.EventsEmit(a.ctx, "transfer_complete", filepath.Join(destPath, receiver.Manifest.FolderName))
				a.AddTransferRecord(receiver.Manifest.FolderName, receiver.Manifest.TotalSize, "receive", "complete")
				return
			}

			lastErr = err
			if !transfer.IsRetryableError(err) {
				break
			}

			time.Sleep(time.Duration(1<<attempt) * time.Second)
		}

		runtime.EventsEmit(a.ctx, "error", fmt.Sprintf("Receive failed after retries: %v", lastErr))
	}()

	return nil
}

func (a *App) startSimulatedSender(path string) (string, error) {
	go func() {
		runtime.EventsEmit(a.ctx, "sender_status", "Initializing Simulation...")
		time.Sleep(1 * time.Second)

		// Fake Manifest
		fakeFiles := []transfer.FileEntry{
			{Path: "simulation_video.mp4", Size: 500 * 1024 * 1024}, // 500MB
			{Path: "simulation_doc.pdf", Size: 5 * 1024 * 1024},     // 5MB
		}
		var totalSize int64 = 505 * 1024 * 1024

		runtime.EventsEmit(a.ctx, "transfer_manifest", map[string]interface{}{
			"folderName": "Simulation Transfer",
			"files":      fakeFiles,
			"totalSize":  totalSize,
		})

		code := "DEV-SIM-123"
		runtime.EventsEmit(a.ctx, "sender_ready", code)
		runtime.EventsEmit(a.ctx, "sender_status", "Waiting for connection (Simulation)...")

		time.Sleep(2 * time.Second)
		runtime.EventsEmit(a.ctx, "log", "Peer connected: SIMULATOR")

		a.simulateFileTransfer(fakeFiles, totalSize, "send", true)
	}()
	return "", nil
}

func (a *App) startSimulatedReceiver(code, destPath string) error {
	go func() {
		runtime.EventsEmit(a.ctx, "log", "Bootstrapping Simulation...")
		time.Sleep(1 * time.Second)
		runtime.EventsEmit(a.ctx, "log", "Finding peer...")
		time.Sleep(1 * time.Second)
		runtime.EventsEmit(a.ctx, "log", "Connecting...")
		time.Sleep(1 * time.Second)

		// Fake Manifest
		fakeFiles := []transfer.FileEntry{
			{Path: "simulation_video.mp4", Size: 500 * 1024 * 1024},
			{Path: "simulation_doc.pdf", Size: 5 * 1024 * 1024},
		}
		var totalSize int64 = 505 * 1024 * 1024

		runtime.EventsEmit(a.ctx, "transfer_manifest", map[string]interface{}{
			"folderName": "Simulation Transfer",
			"totalSize":  totalSize,
			"fileCount":  len(fakeFiles),
			"files":      fakeFiles,
		})

		if a.simulateFileTransfer(fakeFiles, totalSize, "receive", false) {
			// Transfer completed successfully
			runtime.EventsEmit(a.ctx, "transfer_complete", filepath.Join(destPath, "Simulation Transfer"))
		}
	}()
	return nil
}
