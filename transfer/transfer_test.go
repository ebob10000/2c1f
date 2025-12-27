package transfer

import (
	"io"
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
		// Compression is enabled by default

		if err := sender.Handshake(p2); err != nil {
			t.Errorf("Sender handshake failed: %v", err)
			return
		}

		// Wrap stream if compression is enabled (default)
		var dataStream io.ReadWriter = p2
		// Wait, io.ReadWriter is interface.
		// We need to implement the wrapping logic here
		if !sender.NoCompress {
			compressed, err := NewCompressedStream(p2)
			if err != nil {
				t.Errorf("Failed to create compressed stream: %v", err)
				return
			}
			defer compressed.Close()
			dataStream = compressed
		}

		if err := sender.Send(dataStream); err != nil {
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

func TestTransferSingleFile(t *testing.T) {
	// Setup source file
	srcDir := t.TempDir()
	fileName := "single.txt"
	content := "This is a single file transfer"
	srcPath := filepath.Join(srcDir, fileName)
	if err := os.WriteFile(srcPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
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
		receiver.Code = "123-456"
		err := receiver.Receive(p1)
		errChan <- err
	}()

	// Run Sender
	go func() {
		defer p2.Close()
		sender, err := NewSender(srcPath) // Pass file path directly
		if err != nil {
			t.Errorf("Failed to create sender: %v", err)
			return
		}
		sender.Code = "123-456"

		if err := sender.Handshake(p2); err != nil {
			t.Errorf("Sender handshake failed: %v", err)
			return
		}

		var dataStream io.ReadWriter = p2
		if !sender.NoCompress {
			compressed, err := NewCompressedStream(p2)
			if err != nil {
				t.Errorf("Failed to create compressed stream: %v", err)
				return
			}
			defer compressed.Close()
			dataStream = compressed
		}

		if err := sender.Send(dataStream); err != nil {
			t.Errorf("Sender failed: %v", err)
			return
		}
	}()

	// Wait for receiver
	if err := <-errChan; err != nil {
		t.Fatalf("Receiver failed: %v", err)
	}

	// Verify file
	// Receiver creates a folder named after the file (or parent folder? let's check manifest)
	// BuildManifest for file uses filepath.Base(path) as FolderName.
	// So dest will be destDir/single.txt/single.txt ?
	// No, Wait. Manifest.FolderName is Base(path).
	// If path is /tmp/single.txt, FolderName is single.txt.
	// FileEntry path is single.txt.
	// Receiver joins DestPath + Manifest.FolderName + FileEntry.Path
	// -> destDir/single.txt/single.txt
	
	// Let's verify this behavior is what we want.
	// Usually for single file, we might want it directly in DestDir?
	// But current logic enforces a folder structure.
	
	fullPath := filepath.Join(destDir, fileName, fileName)
	data, err := os.ReadFile(fullPath)
	if err != nil {
		t.Fatalf("Failed to read received file %s: %v", fullPath, err)
	}
	if string(data) != content {
		t.Errorf("Content mismatch: got %q, want %q", string(data), content)
	}
}
