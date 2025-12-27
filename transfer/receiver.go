package transfer

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Receiver handles receiving files from a peer
type Receiver struct {
	DestPath   string
	Code       string
	Manifest   *Manifest
	OnStartFile func(filename string, index, total int)
	OnProgress func(filename string, received, total int64)
	OnConfirmation func(m *Manifest) bool
}

// NewReceiver creates a new receiver
func NewReceiver(destPath string) *Receiver {
	return &Receiver{
		DestPath: destPath,
	}
}

// Receive reads files from the stream and saves them
func (r *Receiver) Receive(stream io.ReadWriteCloser) error {
	// 1. Send Handshake
	if err := WriteMessage(stream, &Message{Type: MsgHandshake, Payload: []byte(r.Code)}); err != nil {
		return fmt.Errorf("failed to send handshake: %w", err)
	}

	// 2. Read Handshake Response (expecting Ack or Error)
	msg, err := ReadMessage(stream)
	if err != nil {
		return fmt.Errorf("failed to read handshake response: %w", err)
	}

	if msg.Type == MsgError {
		return fmt.Errorf("handshake rejected: %s", string(msg.Payload))
	}

	if msg.Type != MsgHandshakeAck {
		return fmt.Errorf("expected handshake ack, got %d", msg.Type)
	}

	var ack HandshakeAckMsg
	if err := json.Unmarshal(msg.Payload, &ack); err != nil {
		return fmt.Errorf("invalid handshake ack: %w", err)
	}

	// 3. Setup Compression if needed
	var dataStream io.ReadWriter = stream
	if ack.Compress {
		compressed, err := NewCompressedStream(stream)
		if err != nil {
			return fmt.Errorf("failed to initialize compression: %w", err)
		}
		defer compressed.Close()
		dataStream = compressed
	}

	// 4. Read Manifest
	msg, err = ReadMessage(dataStream)
	if err != nil {
		return fmt.Errorf("failed to read manifest: %w", err)
	}

	if msg.Type == MsgError {
		return fmt.Errorf("handshake rejected: %s", string(msg.Payload))
	}

	manifest, err := ParseManifest(msg)
	if err != nil {
		return err
	}
	r.Manifest = manifest

	if r.OnConfirmation != nil {
		if !r.OnConfirmation(manifest) {
			WriteMessage(dataStream, &Message{Type: MsgError, Payload: []byte("Transfer rejected by receiver")})
			return fmt.Errorf("transfer rejected by user")
		}
	}

	destFolder := filepath.Join(r.DestPath, manifest.FolderName)
	if !strings.HasPrefix(destFolder, filepath.Clean(r.DestPath)) {
		return fmt.Errorf("invalid folder name: %s", manifest.FolderName)
	}
	
	// Calculate resume offsets
	resumeOffsets := make(map[string]int64)
	var existingSize int64

	for _, file := range manifest.Files {
		localPath := filepath.Join(destFolder, filepath.FromSlash(file.Path))
		if !strings.HasPrefix(localPath, filepath.Clean(destFolder)) {
			// Skip invalid paths or return error? Return error is safer.
			return fmt.Errorf("invalid file path in manifest: %s", file.Path)
		}
		offset, _ := verifyLocalFile(localPath, file)
		if offset > 0 {
			resumeOffsets[file.Path] = offset
			existingSize += offset
		}
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
	if err := WriteMessage(dataStream, &Message{Type: MsgResume, Payload: resumeData}); err != nil {
		return fmt.Errorf("failed to send resume message: %w", err)
	}

	fileCount := 0
	for {
		msg, err := ReadMessage(dataStream)
		if err != nil {
			return fmt.Errorf("failed to read message: %w", err)
		}

		switch msg.Type {
		case MsgFileStart:
			fileCount++
			if err := r.receiveFile(dataStream, msg, destFolder, fileCount, len(manifest.Files)); err != nil {
				return err
			}

		case MsgComplete:
			return nil

		case MsgError:
			return fmt.Errorf("sender error: %s", string(msg.Payload))

		default:
			return fmt.Errorf("unexpected message type: %d", msg.Type)
		}
	}
}

func verifyLocalFile(path string, entry FileEntry) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}

	// If no block hashes, fallback to simple size check
	if len(entry.BlockHashes) == 0 {
		if info.Size() > entry.Size {
			return 0, nil // Local file is larger, treat as invalid
		}
		return info.Size(), nil
	}

	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	buf := make([]byte, BlockSize)
	var validatedOffset int64

	for _, expectedHash := range entry.BlockHashes {
		n, err := io.ReadFull(f, buf)
		if err == io.EOF {
			break
		}
		if err == io.ErrUnexpectedEOF {
			// Partial read at the end of file
			if n > 0 {
				hash := sha256.Sum256(buf[:n])
				if hex.EncodeToString(hash[:]) == expectedHash {
					validatedOffset += int64(n)
				}
			}
			break
		}
		if err != nil {
			break
		}

		// Full block read
		hash := sha256.Sum256(buf[:n])
		if hex.EncodeToString(hash[:]) == expectedHash {
			validatedOffset += int64(n)
		} else {
			// Mismatch found, stop verification
			break
		}
	}

	return validatedOffset, nil
}

func (r *Receiver) receiveFile(stream io.Reader, startMsg *Message, destFolder string, current, total int) error {
	var fileStart FileStartMsg
	if err := json.Unmarshal(startMsg.Payload, &fileStart); err != nil {
		return err
	}

	// Find entry for checksum
	var entry *FileEntry
	for i := range r.Manifest.Files {
		if r.Manifest.Files[i].Path == fileStart.Path {
			entry = &r.Manifest.Files[i]
			break
		}
	}

	if r.OnStartFile != nil {
		r.OnStartFile(fileStart.Path, current, total)
	}

	// Skip if fully downloaded
	if fileStart.Offset == fileStart.Size {
		// Even if skipped, we need to read the MsgFileEnd that the sender sends
		endMsg, err := ReadMessage(stream)
		if err != nil {
			return fmt.Errorf("failed to read end message: %w", err)
		}
		if endMsg.Type != MsgFileEnd {
			return fmt.Errorf("expected file end message, got %d", endMsg.Type)
		}
		// TODO: verify checksum of existing file? For now assuming it's correct if size matches.
		return nil
	}

	filePath := filepath.Join(destFolder, filepath.FromSlash(fileStart.Path))
	if !strings.HasPrefix(filePath, filepath.Clean(destFolder)) {
		return fmt.Errorf("invalid file path: %s", fileStart.Path)
	}

	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		return err
	}

	hasher := sha256.New()

	// Handle existing content for hash
	if fileStart.Offset > 0 {
		f, err := os.Open(filePath)
		if err != nil {
			return fmt.Errorf("failed to open existing file for hashing: %w", err)
		}
		// Read only up to offset
		if _, err := io.CopyN(hasher, f, fileStart.Offset); err != nil {
			f.Close()
			return fmt.Errorf("failed to hash existing content: %w", err)
		}
		f.Close()
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
			if err := file.Truncate(fileStart.Offset); err != nil {
				return err
			}
		}
	}

	remaining := fileStart.Size - fileStart.Offset

	// We use io.CopyN to read exactly the expected number of bytes
	// Wrap file with MultiWriter to calculate hash while writing
	multiWriter := io.MultiWriter(file, hasher)

	writer := &ProgressWriter{
		Writer: multiWriter,
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

	// Validate Checksum
	if entry != nil && entry.Checksum != "" {
		actualHash := hex.EncodeToString(hasher.Sum(nil))
		if actualHash != entry.Checksum {
			return fmt.Errorf("checksum mismatch for %s: expected %s, got %s", fileStart.Path, entry.Checksum, actualHash)
		}
	}

	return nil
}
