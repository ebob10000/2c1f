package transfer

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Receiver handles receiving files from a peer
type Receiver struct {
	DestPath   string
	Manifest   *Manifest
	OnProgress func(filename string, received, total int64)
}

// NewReceiver creates a new receiver
func NewReceiver(destPath string) *Receiver {
	return &Receiver{
		DestPath: destPath,
	}
}

// Receive reads files from the stream and saves them
func (r *Receiver) Receive(stream io.ReadWriter) error {
	msg, err := ReadMessage(stream)
	if err != nil {
		return fmt.Errorf("failed to read manifest: %w", err)
	}

	manifest, err := ParseManifest(msg)
	if err != nil {
		return err
	}
	r.Manifest = manifest

	destFolder := filepath.Join(r.DestPath, manifest.FolderName)
	
	// Calculate resume offsets
	resumeOffsets := make(map[string]int64)
	var existingSize int64

	for _, file := range manifest.Files {
		localPath := filepath.Join(destFolder, filepath.FromSlash(file.Path))
		info, err := os.Stat(localPath)
		if err == nil && !info.IsDir() {
			if info.Size() < file.Size {
				resumeOffsets[file.Path] = info.Size()
				existingSize += info.Size()
			} else if info.Size() == file.Size {
				resumeOffsets[file.Path] = info.Size()
				existingSize += info.Size()
			}
		}
	}

	fmt.Printf("Receiving: %s (%d files, %s)\n",
		manifest.FolderName,
		len(manifest.Files),
		formatBytes(manifest.TotalSize))
	
	if existingSize > 0 {
		fmt.Printf("Resuming transfer... found %s existing data\n", formatBytes(existingSize))
	}

	if err := os.MkdirAll(destFolder, 0755); err != nil {
		return fmt.Errorf("failed to create destination folder: %w", err)
	}

	// Send ResumeMsg
	resumeMsg := ResumeMsg{Files: resumeOffsets}
	resumeData, err := json.Marshal(resumeMsg)
	if err != nil {
		return err
	}
	if err := WriteMessage(stream, &Message{Type: MsgResume, Payload: resumeData}); err != nil {
		return fmt.Errorf("failed to send resume message: %w", err)
	}

	fileCount := 0
	for {
		msg, err := ReadMessage(stream)
		if err != nil {
			return fmt.Errorf("failed to read message: %w", err)
		}

		switch msg.Type {
		case MsgFileStart:
			fileCount++
			if err := r.receiveFile(stream, msg, destFolder, fileCount, len(manifest.Files)); err != nil {
				return err
			}

		case MsgComplete:
			fmt.Println("Transfer complete!")
			return nil

		case MsgError:
			return fmt.Errorf("sender error: %s", string(msg.Payload))

		default:
			return fmt.Errorf("unexpected message type: %d", msg.Type)
		}
	}
}

func (r *Receiver) receiveFile(stream io.Reader, startMsg *Message, destFolder string, current, total int) error {
	var fileStart FileStartMsg
	if err := json.Unmarshal(startMsg.Payload, &fileStart); err != nil {
		return err
	}

	// Skip if fully downloaded
	if fileStart.Offset == fileStart.Size {
		fmt.Printf("[%d/%d] Skipping: %s (already downloaded)\n", current, total, fileStart.Path)
		// Even if skipped, we need to read the MsgFileEnd that the sender sends
		endMsg, err := ReadMessage(stream)
		if err != nil {
			return fmt.Errorf("failed to read end message: %w", err)
		}
		if endMsg.Type != MsgFileEnd {
			return fmt.Errorf("expected file end message, got %d", endMsg.Type)
		}
		return nil
	}

	if fileStart.Offset > 0 {
		fmt.Printf("[%d/%d] Resuming: %s (from %s)\n", current, total, fileStart.Path, formatBytes(fileStart.Offset))
	} else {
		fmt.Printf("[%d/%d] Receiving: %s\n", current, total, fileStart.Path)
	}

	filePath := filepath.Join(destFolder, filepath.FromSlash(fileStart.Path))

	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		return err
	}

	// Open file in append mode if resuming, create/truncate otherwise
	flags := os.O_CREATE | os.O_WRONLY
	if fileStart.Offset > 0 {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}

	file, err := os.OpenFile(filePath, flags, 0644)
	if err != nil {
		return err
	}
	defer file.Close()
	
	// If resuming, verify we are at the correct offset
	if fileStart.Offset > 0 {
		pos, err := file.Seek(0, io.SeekEnd)
		if err != nil {
			return err
		}
		if pos != fileStart.Offset {
			// This shouldn't happen if we calculated offsets correctly based on local files,
			// but if the file changed between manifest and now, it's safer to truncate.
			// However, simplified logic: just error out or warn.
			// Let's truncate to the expected offset to be safe
			if err := file.Truncate(fileStart.Offset); err != nil {
				return err
			}
		}
	}

	remaining := fileStart.Size - fileStart.Offset

	// We use io.CopyN to read exactly the expected number of bytes
	writer := &ProgressWriter{
		Writer: file,
		Total:  fileStart.Size,
		Current: fileStart.Offset,
		OnProgress: func(current, total int64) {
			if r.OnProgress != nil {
				r.OnProgress(fileStart.Path, current, total)
			}
		},
	}

	copied, err := io.CopyN(writer, stream, remaining)
	if err != nil {
		return fmt.Errorf("failed to read file data: %w", err)
	}

	if copied != remaining {
		return fmt.Errorf("unexpected EOF: read %d of %d bytes", copied, remaining)
	}

	endMsg, err := ReadMessage(stream)
	if err != nil {
		return fmt.Errorf("failed to read end message: %w", err)
	}
	if endMsg.Type != MsgFileEnd {
		return fmt.Errorf("expected file end message, got %d", endMsg.Type)
	}

	return nil
}
