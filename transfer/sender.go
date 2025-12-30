package transfer

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

const ChunkSize = 64 * 1024

type Sender struct {
	FolderPath  string
	Code        string
	Compress    bool
	Manifest    *Manifest
	OnStartFile func(filename string, index, total int)
	OnProgress  func(filename string, sent, total int64)
}

func NewSender(folderPath string, cacheManifest bool, skipHash bool, onProgress ManifestProgressFunc) (*Sender, error) {
	manifest, err := BuildManifest(folderPath, cacheManifest, skipHash, onProgress)
	if err != nil {
		return nil, err
	}

	return &Sender{
		FolderPath: folderPath,
		Manifest:   manifest,
		Compress:   false,
	}, nil
}

func (s *Sender) Handshake(stream io.ReadWriter) error {
	SetStreamDeadline(stream, StreamTimeout)
	msg, err := ReadMessage(stream)
	if err != nil {
		return fmt.Errorf("failed to read handshake: %w", err)
	}
	if msg.Type != MsgHandshake {
		return fmt.Errorf("expected handshake, got %d", msg.Type)
	}

	var handshake HandshakeMsg
	if err := json.Unmarshal(msg.Payload, &handshake); err != nil {
		if string(msg.Payload) != s.Code {
			errMsg := "invalid connection code"
			WriteMessage(stream, &Message{Type: MsgError, Payload: []byte(errMsg)})
			return errors.New(errMsg)
		}
	} else {
		if handshake.Code != s.Code {
			errMsg := "invalid connection code"
			WriteMessage(stream, &Message{Type: MsgError, Payload: []byte(errMsg)})
			return errors.New(errMsg)
		}
	}

	ack := HandshakeAckMsg{Compress: s.Compress}
	ackData, err := json.Marshal(ack)
	if err != nil {
		return fmt.Errorf("failed to marshal handshake ack: %w", err)
	}
	if err := WriteMessage(stream, &Message{Type: MsgHandshakeAck, Payload: ackData}); err != nil {
		return fmt.Errorf("failed to send handshake ack: %w", err)
	}

	return nil
}

func (s *Sender) Send(stream io.ReadWriter) error {
	if err := SendManifest(stream, s.Manifest); err != nil {
		return fmt.Errorf("failed to send manifest: %w", err)
	}

	SetStreamDeadline(stream, StreamTimeout)
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

	bufferedStream := &BufferedDeadlineWriter{
		Writer:     bufio.NewWriterSize(stream, 1024*1024),
		Underlying: stream,
	}
	defer bufferedStream.Flush()

	for i, file := range s.Manifest.Files {
		offset := resumeMsg.Files[file.Path]

		if offset >= file.Size {
			offset = file.Size
		}

		if s.OnStartFile != nil {
			s.OnStartFile(file.Path, i+1, len(s.Manifest.Files))
		}

		if err := s.sendFile(bufferedStream, file, offset); err != nil {
			return fmt.Errorf("failed to send %s: %w", file.Path, err)
		}
	}

	bufferedStream.Flush()

	if err := WriteMessage(stream, &Message{Type: MsgComplete}); err != nil {
		return fmt.Errorf("failed to send completion: %w", err)
	}

	if s, ok := stream.(interface{ SetReadDeadline(time.Time) error }); ok {
		s.SetReadDeadline(time.Now().Add(10 * time.Second))
	}

	buf := make([]byte, 1)
	if _, readErr := stream.Read(buf); readErr != nil && readErr != io.EOF {
		// This is just a courtesy wait for receiver acknowledgment
		// Log the warning but don't fail the transfer since data was already sent
		fmt.Fprintf(os.Stderr, "Warning: receiver may not have acknowledged file completion: %v\n", readErr)
	}

	return nil
}

func (s *Sender) sendFile(stream io.Writer, entry FileEntry, offset int64) error {
	startMsg := FileStartMsg{Path: entry.Path, Size: entry.Size, Offset: offset}
	startData, err := json.Marshal(startMsg)
	if err != nil {
		return fmt.Errorf("failed to marshal file start message: %w", err)
	}
	if err := WriteMessage(stream, &Message{Type: MsgFileStart, Payload: startData}); err != nil {
		return err
	}

	if offset == entry.Size {
		return WriteMessage(stream, &Message{Type: MsgFileEnd})
	}

	var filePath string
	info, err := os.Stat(s.FolderPath)
	if err == nil && !info.IsDir() {
		filePath = s.FolderPath
	} else {
		filePath = filepath.Join(s.FolderPath, filepath.FromSlash(entry.Path))
	}

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

	remaining := entry.Size - offset
	currentPos := offset

	buf := make([]byte, 256*1024)

	timeoutStream := &TimeoutWriter{W: stream, Timeout: StreamTimeout}

	for remaining > 0 {
		toRead := int64(len(buf))
		if toRead > remaining {
			toRead = remaining
		}

		n, readErr := file.Read(buf[:toRead])
		if n > 0 {
			written := 0
			for written < n {
				wn, writeErr := timeoutStream.Write(buf[written:n])
				if writeErr != nil {
					return fmt.Errorf("failed to copy file data: %w", writeErr)
				}
				if wn == 0 {
					return fmt.Errorf("failed to copy file data: zero bytes written")
				}
				written += wn
			}

			currentPos += int64(n)
			remaining -= int64(n)

			if s.OnProgress != nil {
				s.OnProgress(entry.Path, currentPos, entry.Size)
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
		return fmt.Errorf("incomplete transfer: sent %d of %d bytes", entry.Size-offset-remaining, entry.Size-offset)
	}

	return WriteMessage(stream, &Message{Type: MsgFileEnd})
}

func FormatBytes(bytes int64) string {
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
