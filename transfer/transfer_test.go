package transfer

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

type MockStream struct {
	*io.PipeWriter
}

func (m *MockStream) Read(p []byte) (n int, err error) {
	select {} // Block forever as we don't expect sender to read
}

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
	r, w := io.Pipe()

	senderStream := &MockStream{PipeWriter: w}

	// Run Receiver in a goroutine
	errChan := make(chan error, 1)
	go func() {
		receiver := NewReceiver(destDir)
		err := receiver.Receive(r)
		errChan <- err
	}()

	// Run Sender
	sender, err := NewSender(srcDir)
	if err != nil {
		t.Fatalf("Failed to create sender: %v", err)
	}

	if err := sender.Send(senderStream); err != nil {
		t.Fatalf("Sender failed: %v", err)
	}
	w.Close() // Close writer to signal EOF to receiver if it keeps reading

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
