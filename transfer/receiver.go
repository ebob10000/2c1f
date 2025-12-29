package transfer

import (
	"bufio"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"lukechampine.com/blake3"
)

type Receiver struct {
	DestPath       string
	Code           string
	Manifest       *Manifest
	FastResume     bool
	OnStartFile    func(filename string, index, total int)
	OnProgress     func(filename string, received, total int64)
	OnConfirmation func(m *Manifest) bool
}

func NewReceiver(destPath string) *Receiver {
	return &Receiver{
		DestPath: destPath,
	}
}

func (r *Receiver) Receive(stream io.ReadWriteCloser) error {
	SetStreamDeadline(stream, StreamTimeout)
	if err := WriteMessage(stream, &Message{Type: MsgHandshake, Payload: []byte(r.Code)}); err != nil {
		return fmt.Errorf("failed to send handshake: %w", err)
	}

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

	var dataStream io.ReadWriter = stream
	if ack.Compress {
		compressed, err := NewCompressedStream(stream)
		if err != nil {
			return fmt.Errorf("failed to initialize compression: %w", err)
		}
		defer compressed.Close()
		dataStream = compressed
	}

	SetStreamDeadline(stream, StreamTimeout)
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

	resumeOffsets := make(map[string]int64)
	var existingSize int64

	for _, file := range manifest.Files {
		localPath := filepath.Join(destFolder, filepath.FromSlash(file.Path))
		if !strings.HasPrefix(localPath, filepath.Clean(destFolder)) {
			return fmt.Errorf("invalid file path in manifest: %s", file.Path)
		}
		offset, _ := r.verifyLocalFile(localPath, file)
		if offset > 0 {
			resumeOffsets[file.Path] = offset
			existingSize += offset
		}
	}

	if err := os.MkdirAll(destFolder, 0755); err != nil {
		return fmt.Errorf("failed to create destination folder: %w", err)
	}

	resumeMsg := ResumeMsg{Files: resumeOffsets}
	resumeData, err := json.Marshal(resumeMsg)
	if err != nil {
		return err
	}
	if err := WriteMessage(dataStream, &Message{Type: MsgResume, Payload: resumeData}); err != nil {
		return fmt.Errorf("failed to send resume message: %w", err)
	}

	bufferedStream := &BufferedDeadlineReader{
		Reader:     bufio.NewReaderSize(dataStream, 1024*1024),
		Underlying: dataStream,
	}

	fileCount := 0
	for {
		SetStreamDeadline(stream, StreamTimeout)
		msg, err := ReadMessage(bufferedStream)
		if err != nil {
			return fmt.Errorf("failed to read message: %w", err)
		}

		switch msg.Type {
		case MsgFileStart:
			fileCount++
			if err := r.receiveFile(bufferedStream, msg, destFolder, fileCount, len(manifest.Files)); err != nil {
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

func (r *Receiver) verifyLocalFile(path string, entry FileEntry) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}

	if r.FastResume {
		if info.Size() > entry.Size {
			return 0, nil
		}
		return info.Size(), nil
	}

	if len(entry.BlockHashes) == 0 {
		if info.Size() > entry.Size {
			return 0, nil
		}
		return info.Size(), nil
	}

	blockSize := entry.BlockSize
	if blockSize == 0 {
		blockSize = LegacyBlockSize
	}

	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	buf := make([]byte, blockSize)
	var validatedOffset int64

	for _, expectedHash := range entry.BlockHashes {
		n, err := io.ReadFull(f, buf)
		if err == io.EOF {
			break
		}
		if err == io.ErrUnexpectedEOF {
			if n > 0 {
				hash := blake3.Sum256(buf[:n])
				if hex.EncodeToString(hash[:]) == expectedHash {
					validatedOffset += int64(n)
				}
			}
			break
		}
		if err != nil {
			break
		}

		hash := blake3.Sum256(buf[:n])
		if hex.EncodeToString(hash[:]) == expectedHash {
			validatedOffset += int64(n)
		} else {
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

	if fileStart.Offset == fileStart.Size {
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

	filePath := filepath.Join(destFolder, filepath.FromSlash(fileStart.Path))
	cleanDest := filepath.Clean(destFolder)
	cleanPath := filepath.Clean(filePath)

	if !strings.HasPrefix(cleanPath, cleanDest+string(os.PathSeparator)) {
		return fmt.Errorf("invalid file path (Zip Slip detected): %s", fileStart.Path)
	}

	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		return fmt.Errorf("failed to create directory for %s: %w", filePath, err)
	}

	hasher := blake3.New(32, nil)

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

	flags := os.O_CREATE | os.O_WRONLY
	if fileStart.Offset > 0 {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}

	file, err := os.OpenFile(filePath, flags, 0644)
	if err != nil {
		return fmt.Errorf("failed to open file %s: %w", filePath, err)
	}
	defer file.Close()

	if fileStart.Offset > 0 {
		pos, err := file.Seek(0, io.SeekEnd)
		if err != nil {
			return err
		}
		if pos != fileStart.Offset {
			if err := file.Truncate(fileStart.Offset); err != nil {
				return err
			}
		}
	}

	remaining := fileStart.Size - fileStart.Offset
	currentPos := fileStart.Offset

	multiWriter := io.MultiWriter(file, hasher)

	timeoutStream := &TimeoutReader{R: stream, Timeout: StreamTimeout}

	buf := make([]byte, 256*1024)

	for remaining > 0 {
		toRead := int64(len(buf))
		if toRead > remaining {
			toRead = remaining
		}

		n, readErr := timeoutStream.Read(buf[:toRead])
		if n > 0 {
			written := 0
			for written < n {
				wn, writeErr := multiWriter.Write(buf[written:n])
				if writeErr != nil {
					return fmt.Errorf("failed to write file data: %w", writeErr)
				}
				if wn == 0 {
					return fmt.Errorf("failed to write file data: zero bytes written")
				}
				written += wn
			}

			currentPos += int64(n)
			remaining -= int64(n)

			if r.OnProgress != nil {
				r.OnProgress(fileStart.Path, currentPos, fileStart.Size)
			}
		}

		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			return fmt.Errorf("failed to read file data: %w", readErr)
		}
	}

	if remaining != 0 {
		return fmt.Errorf("unexpected EOF: read %d of %d bytes", fileStart.Size-fileStart.Offset-remaining, fileStart.Size-fileStart.Offset)
	}

	endMsg, err := ReadMessage(stream)
	if err != nil {
		return fmt.Errorf("failed to read end message: %w", err)
	}
	if endMsg.Type != MsgFileEnd {
		return fmt.Errorf("expected file end message, got %d", endMsg.Type)
	}

	if entry != nil && entry.Checksum != "" {
		actualHash := hex.EncodeToString(hasher.Sum(nil))
		if actualHash != entry.Checksum {
			return fmt.Errorf("checksum mismatch for %s: expected %s, got %s", fileStart.Path, entry.Checksum, actualHash)
		}
	}

	return nil
}
