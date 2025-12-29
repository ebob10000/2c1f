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

	// Use TCP listener instead of net.Pipe to avoid deadlock with synchronous gzip header exchange
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	errChan := make(chan error, 1)

	// Run Receiver
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			errChan <- err
			return
		}
		defer conn.Close()

		receiver := NewReceiver(destDir)
		receiver.Code = "123-456" // Set a code for handshake
		errChan <- receiver.Receive(conn)
	}()

	// Run Sender
	go func() {
		conn, err := net.Dial("tcp", ln.Addr().String())
		if err != nil {
			t.Errorf("Failed to connect: %v", err)
			return
		}
		defer conn.Close()

		sender, err := NewSender(srcDir, false, false, nil)
		if err != nil {
			t.Errorf("Failed to create sender: %v", err)
			return
		}
		sender.Code = "123-456" // Match the code
		sender.Compress = true  // Enable compression for test

		if err := sender.Handshake(conn); err != nil {
			t.Errorf("Sender handshake failed: %v", err)
			return
		}

		// Wrap stream if compression is enabled
		var dataStream io.ReadWriter = conn
		if sender.Compress {
			compressed, err := NewCompressedStream(conn)
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
		// Receiver creates a subfolder with the source folder name
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

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	errChan := make(chan error, 1)

	// Run Receiver
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			errChan <- err
			return
		}
		defer conn.Close()

		receiver := NewReceiver(destDir)
		receiver.Code = "123-456"
		errChan <- receiver.Receive(conn)
	}()

	// Run Sender
	go func() {
		conn, err := net.Dial("tcp", ln.Addr().String())
		if err != nil {
			t.Errorf("Failed to connect: %v", err)
			return
		}
		defer conn.Close()

		sender, err := NewSender(srcPath, false, false, nil) // Pass file path directly
		if err != nil {
			t.Errorf("Failed to create sender: %v", err)
			return
		}
		sender.Code = "123-456"
		sender.Compress = true // Enable compression

		if err := sender.Handshake(conn); err != nil {
			t.Errorf("Sender handshake failed: %v", err)
			return
		}

		var dataStream io.ReadWriter = conn
		if sender.Compress {
			compressed, err := NewCompressedStream(conn)
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

	fullPath := filepath.Join(destDir, fileName, fileName)
	data, err := os.ReadFile(fullPath)
	if err != nil {
		t.Fatalf("Failed to read received file %s: %v", fullPath, err)
	}
	if string(data) != content {
		t.Errorf("Content mismatch: got %q, want %q", string(data), content)
	}
}