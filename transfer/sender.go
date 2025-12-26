package transfer

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const ChunkSize = 64 * 1024 // 64KB chunks

// Sender handles sending files to a peer
type Sender struct {
	FolderPath string
	Code       string
	Manifest   *Manifest
	OnStartFile func(filename string, index, total int)
	OnProgress func(filename string, sent, total int64)
}

// NewSender creates a new sender for the given folder
func NewSender(folderPath string) (*Sender, error) {
	manifest, err := BuildManifest(folderPath)
	if err != nil {
		return nil, err
	}

	return &Sender{
		FolderPath: folderPath,
		Manifest:   manifest,
	}, nil
}

// Send transfers all files over the stream
func (s *Sender) Send(stream io.ReadWriter) error {
	// 1. Wait for Handshake
	msg, err := ReadMessage(stream)
	if err != nil {
		return fmt.Errorf("failed to read handshake: %w", err)
	}
	if msg.Type != MsgHandshake {
		return fmt.Errorf("expected handshake, got %d", msg.Type)
	}
	if string(msg.Payload) != s.Code {
		errMsg := "invalid connection code"
		WriteMessage(stream, &Message{Type: MsgError, Payload: []byte(errMsg)})
		return fmt.Errorf(errMsg)
	}

	fmt.Printf("Sending manifest: %s (%d files, %s)\n",
		s.Manifest.FolderName,
		len(s.Manifest.Files),
		formatBytes(s.Manifest.TotalSize))

	if err := SendManifest(stream, s.Manifest); err != nil {
		return fmt.Errorf("failed to send manifest: %w", err)
	}

	// Wait for Resume message
	msg, err := ReadMessage(stream)
	if err != nil {
		return fmt.Errorf("failed to receive resume message: %w", err)
	}
	
	if msg.Type != MsgResume {
		return fmt.Errorf("expected resume message, got %d", msg.Type)
	}

	var resumeMsg ResumeMsg
	if err := json.Unmarshal(msg.Payload, &resumeMsg); err != nil {
		return fmt.Errorf("invalid resume message: %w", err)
	}

	for i, file := range s.Manifest.Files {
		offset := resumeMsg.Files[file.Path]
		
		// If file is fully transferred, skip (but send metadata so receiver knows to skip)
		if offset >= file.Size {
			offset = file.Size // Mark as complete
		}

		if s.OnStartFile != nil {
			s.OnStartFile(file.Path, i+1, len(s.Manifest.Files))
		}

		if err := s.sendFile(stream, file, offset); err != nil {
			return fmt.Errorf("failed to send %s: %w", file.Path, err)
		}
	}

	if err := WriteMessage(stream, &Message{Type: MsgComplete}); err != nil {
		return fmt.Errorf("failed to send completion: %w", err)
	}

	fmt.Println("Waiting for receiver to finish...")
	// Wait for the receiver to process the completion message and close the stream.
	// This ensures the connection isn't torn down while data is still buffered.
	io.ReadAll(stream)

	fmt.Println("Transfer complete!")
	return nil
}

func (s *Sender) sendFile(stream io.ReadWriter, entry FileEntry, offset int64) error {
	// Send file start message first
	startMsg := FileStartMsg{Path: entry.Path, Size: entry.Size, Offset: offset}
	startData, _ := json.Marshal(startMsg)
	if err := WriteMessage(stream, &Message{Type: MsgFileStart, Payload: startData}); err != nil {
		return err
	}

	// If fully skipped, just send End message
	if offset == entry.Size {
		return WriteMessage(stream, &Message{Type: MsgFileEnd})
	}

	filePath := filepath.Join(s.FolderPath, filepath.FromSlash(entry.Path))

	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	if offset > 0 {
		if _, err := file.Seek(offset, io.SeekStart); err != nil {
			return err
		}
	}

	reader := &ProgressReader{
		Reader: file,
		Total:  entry.Size,
		Current: offset,
		OnProgress: func(current, total int64) {
			if s.OnProgress != nil {
				s.OnProgress(entry.Path, current, total)
			}
		},
	}

	sent, err := io.Copy(stream, reader)
	if err != nil {
		return fmt.Errorf("failed to copy file data: %w", err)
	}
	if sent != (entry.Size - offset) {
		return fmt.Errorf("incomplete transfer: sent %d of %d bytes", sent, entry.Size-offset)
	}

	return WriteMessage(stream, &Message{Type: MsgFileEnd})
}

func formatBytes(bytes int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)

	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.2f GB", float64(bytes)/GB)
	case bytes >= MB:
		return fmt.Sprintf("%.2f MB", float64(bytes)/MB)
	case bytes >= KB:
		return fmt.Sprintf("%.2f KB", float64(bytes)/KB)
	default:
		return fmt.Sprintf("%d bytes", bytes)
	}
}
