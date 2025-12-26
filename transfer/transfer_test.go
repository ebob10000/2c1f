package transfer

import (
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestTransfer(t *testing.T) {
	// Setup source folder with some files
	srcDir := t.TempDir()
	files := map[string]string{
		"file1.txt":       "Hello World",
		"subdir/file2.go": "package main",
		"large.bin":       string(make([]byte, 1024*1024)), // 1MB dummy file
	}

	for path, content := range files {
		fullPath := filepath.Join(srcDir, path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Setup destination folder
	destDir := t.TempDir()

	// Create pipe to simulate network connection
	p1, p2 := net.Pipe()

	// Run Receiver in a goroutine
	errChan := make(chan error, 1)
	go func() {
		defer p1.Close()
		receiver := NewReceiver(destDir)
		receiver.Code = "123-456" // Set a code for handshake
		err := receiver.Receive(p1)
		errChan <- err
	}()

	// Run Sender
	go func() {
		defer p2.Close()
		sender, err := NewSender(srcDir)
		if err != nil {
			t.Errorf("Failed to create sender: %v", err)
			return
		}
		sender.Code = "123-456" // Match the code
		sender.NoCompress = true

		if err := sender.Handshake(p2); err != nil {
			t.Errorf("Sender handshake failed: %v", err)
			return
		}

		if err := sender.Send(p2); err != nil {
			t.Errorf("Sender failed: %v", err)
			return
		}
	}()

	// Wait for receiver
	if err := <-errChan; err != nil {
		t.Fatalf("Receiver failed: %v", err)
	}

	// Verify files
	for path, content := range files {
		// Note: Receiver creates a subfolder with the source folder name
		fullPath := filepath.Join(destDir, filepath.Base(srcDir), path)
		data, err := os.ReadFile(fullPath)
		if err != nil {
			t.Errorf("Failed to read received file %s: %v", path, err)
			continue
		}
		if string(data) != content {
			if len(content) > 100 {
				t.Errorf("File content mismatch for %s: size %d != %d", path, len(data), len(content))
			} else {
				t.Errorf("File content mismatch for %s: got %q, want %q", path, string(data), content)
			}
		}
	}
}