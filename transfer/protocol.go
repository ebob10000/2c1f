package transfer

import (
	"bufio"
	"compress/gzip"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"lukechampine.com/blake3"
)

// MessageType identifies the type of protocol message
type MessageType uint8

const (
	MsgManifest MessageType = iota
	MsgResume
	MsgFileStart
	MsgFileEnd
	MsgComplete
	MsgError
	MsgHandshake
	MsgHandshakeAck
)

type Message struct {
	Type    MessageType `json:"type"`
	Payload []byte      `json:"payload,omitempty"`
}

type HandshakeMsg struct {
	Code string `json:"code"`
}

type HandshakeAckMsg struct {
	Compress bool `json:"compress"`
}

type Manifest struct {
	FolderName string      `json:"folder_name"`
	TotalSize  int64       `json:"total_size"`
	Files      []FileEntry `json:"files"`
}

type FileEntry struct {
	Path        string      `json:"path"` // Relative path within folder
	Size        int64       `json:"size"`
	Mode        os.FileMode `json:"mode"`
	Checksum    string      `json:"checksum"`
	BlockHashes []string    `json:"block_hashes,omitempty"`
	BlockSize   int64       `json:"block_size,omitempty"`
}

const BlockSize = 16 * 1024 * 1024
const LegacyBlockSize = 1024 * 1024
const MaxMessageSize = 100 << 20
const StreamTimeout = 60 * time.Second
const MaxRetries = 5
const RetryBaseDelay = 2 * time.Second

type ResumeMsg struct {
	Files map[string]int64 `json:"files"` // Path -> Offset
}

// FileStartMsg indicates the beginning of a file transfer
type FileStartMsg struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	Offset int64  `json:"offset,omitempty"`
}

// CompressedStream wraps a stream with gzip compression
type CompressedStream struct {
	r *gzip.Reader
	w *gzip.Writer
	c io.Closer
}

func NewCompressedStream(s io.ReadWriteCloser) (*CompressedStream, error) {
	w := gzip.NewWriter(s)
	if err := w.Flush(); err != nil {
		return nil, err
	}

	r, err := gzip.NewReader(s)
	if err != nil {
		return nil, err
	}

	return &CompressedStream{r: r, w: w, c: s}, nil
}

func (cs *CompressedStream) Read(p []byte) (int, error) {
	return cs.r.Read(p)
}

func (cs *CompressedStream) Write(p []byte) (int, error) {
	return cs.w.Write(p)
}

func (cs *CompressedStream) SetReadDeadline(t time.Time) error {
	if s, ok := cs.c.(interface{ SetReadDeadline(time.Time) error }); ok {
		return s.SetReadDeadline(t)
	}
	return nil
}

func (cs *CompressedStream) SetWriteDeadline(t time.Time) error {
	if s, ok := cs.c.(interface{ SetWriteDeadline(time.Time) error }); ok {
		return s.SetWriteDeadline(t)
	}
	return nil
}

func (cs *CompressedStream) SetDeadline(t time.Time) error {
	if s, ok := cs.c.(interface{ SetDeadline(time.Time) error }); ok {
		return s.SetDeadline(t)
	}
	return nil
}

func (cs *CompressedStream) Close() error {
	if err := cs.w.Close(); err != nil {
		return err
	}
	return cs.c.Close()
}

func (cs *CompressedStream) Flush() error {
	return cs.w.Flush()
}

type BufferedDeadlineReader struct {
	*bufio.Reader
	Underlying io.Reader
}

func (b *BufferedDeadlineReader) SetReadDeadline(t time.Time) error {
	if s, ok := b.Underlying.(interface{ SetReadDeadline(time.Time) error }); ok {
		return s.SetReadDeadline(t)
	}
	return nil
}

func (b *BufferedDeadlineReader) SetWriteDeadline(t time.Time) error {
	if s, ok := b.Underlying.(interface{ SetWriteDeadline(time.Time) error }); ok {
		return s.SetWriteDeadline(t)
	}
	return nil
}

func (b *BufferedDeadlineReader) SetDeadline(t time.Time) error {
	if s, ok := b.Underlying.(interface{ SetDeadline(time.Time) error }); ok {
		return s.SetDeadline(t)
	}
	return nil
}

type BufferedDeadlineWriter struct {
	*bufio.Writer
	Underlying io.Writer
}

func (b *BufferedDeadlineWriter) SetReadDeadline(t time.Time) error {
	if s, ok := b.Underlying.(interface{ SetReadDeadline(time.Time) error }); ok {
		return s.SetReadDeadline(t)
	}
	return nil
}

func (b *BufferedDeadlineWriter) SetWriteDeadline(t time.Time) error {
	if s, ok := b.Underlying.(interface{ SetWriteDeadline(time.Time) error }); ok {
		return s.SetWriteDeadline(t)
	}
	return nil
}

func (b *BufferedDeadlineWriter) SetDeadline(t time.Time) error {
	if s, ok := b.Underlying.(interface{ SetDeadline(time.Time) error }); ok {
		return s.SetDeadline(t)
	}
	return nil
}

func (b *BufferedDeadlineWriter) Flush() error {
	return b.Writer.Flush()
}

type ManifestProgressFunc func(path string, size int64)

func BuildManifest(path string, cache bool, skipHash bool, onProgress ManifestProgressFunc) (*Manifest, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("cannot access path: %w", err)
	}

	manifestFile := filepath.Join(path, ".2c1f_manifest.json")
	if cache && info.IsDir() && !skipHash {
		if data, err := os.ReadFile(manifestFile); err == nil {
			var cachedManifest Manifest
			if err := json.Unmarshal(data, &cachedManifest); err == nil {
				return &cachedManifest, nil
			}
		}
	}

	manifest := &Manifest{
		FolderName: filepath.Base(path),
		Files:      []FileEntry{},
	}

	if !info.IsDir() {
		var hash string
		var blockHashes []string

		if onProgress != nil {
			onProgress(filepath.Base(path), info.Size())
		}

		if !skipHash {
			hash, blockHashes, err = calculateHashAndBlocks(path)
			if err != nil {
				return nil, fmt.Errorf("failed to calculate hash: %w", err)
			}
		}
		manifest.Files = append(manifest.Files, FileEntry{
			Path:        filepath.Base(path),
			Size:        info.Size(),
			Mode:        info.Mode(),
			Checksum:    hash,
			BlockHashes: blockHashes,
			BlockSize:   BlockSize,
		})
		manifest.TotalSize = info.Size()
		return manifest, nil
	}

	var filesToHash []string
	err = filepath.Walk(path, func(walkPath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if filepath.Base(walkPath) == ".2c1f_manifest.json" {
			return nil
		}
		filesToHash = append(filesToHash, walkPath)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to walk folder: %w", err)
	}

	// Process files in parallel
	numWorkers := runtime.NumCPU()
	jobChan := make(chan string, len(filesToHash))
	resultChan := make(chan FileEntry, len(filesToHash))
	errChan := make(chan error, 1)
	var wg sync.WaitGroup

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for walkPath := range jobChan {
				select {
				case <-errChan:
					return
				default:
				}

				info, err := os.Stat(walkPath)
				if err != nil {
					select {
					case errChan <- err:
					default:
					}
					return
				}

				relPath, err := filepath.Rel(path, walkPath)
				if err != nil {
					select {
					case errChan <- err:
					default:
					}
					return
				}

				if onProgress != nil {
					onProgress(relPath, info.Size())
				}

				var hash string
				var blockHashes []string
				if !skipHash {
					hash, blockHashes, err = calculateHashAndBlocks(walkPath)
					if err != nil {
						select {
						case errChan <- err:
						default:
						}
						return
					}
				}

				resultChan <- FileEntry{
					Path:        filepath.ToSlash(relPath),
					Size:        info.Size(),
					Mode:        info.Mode(),
					Checksum:    hash,
					BlockHashes: blockHashes,
					BlockSize:   BlockSize,
				}
			}
		}()
	}

	for _, f := range filesToHash {
		jobChan <- f
	}
	close(jobChan)

	// Wait for workers
	wg.Wait()
	close(resultChan)

	// Check for errors
	select {
	case err := <-errChan:
		return nil, err
	default:
	}

	for entry := range resultChan {
		manifest.Files = append(manifest.Files, entry)
		manifest.TotalSize += entry.Size
	}

	if cache && info.IsDir() && !skipHash {
		data, err := json.MarshalIndent(manifest, "", "  ")
		if err == nil {
			os.WriteFile(manifestFile, data, 0600)
		}
	}

	return manifest, nil
}

func WriteMessage(w io.Writer, msg *Message) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	length := uint32(len(data))
	lengthBytes := []byte{
		byte(length >> 24),
		byte(length >> 16),
		byte(length >> 8),
		byte(length),
	}

	if _, err := w.Write(lengthBytes); err != nil {
		return err
	}
	if _, err := w.Write(data); err != nil {
		return err
	}

	if f, ok := w.(interface{ Flush() error }); ok {
		return f.Flush()
	}

	return nil
}

func ReadMessage(r io.Reader) (*Message, error) {
	lengthBytes := make([]byte, 4)
	if _, err := io.ReadFull(r, lengthBytes); err != nil {
		return nil, err
	}

	length := uint32(lengthBytes[0])<<24 |
		uint32(lengthBytes[1])<<16 |
		uint32(lengthBytes[2])<<8 |
		uint32(lengthBytes[3])

	if length > MaxMessageSize {
		return nil, fmt.Errorf("message too large: %d > %d", length, MaxMessageSize)
	}

	data := make([]byte, length)
	if _, err := io.ReadFull(r, data); err != nil {
		return nil, err
	}

	var msg Message
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, err
	}

	return &msg, nil
}

func SendManifest(w io.Writer, manifest *Manifest) error {
	data, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	return WriteMessage(w, &Message{Type: MsgManifest, Payload: data})
}

func ParseManifest(msg *Message) (*Manifest, error) {
	if msg.Type != MsgManifest {
		return nil, fmt.Errorf("expected manifest message, got %d", msg.Type)
	}
	var manifest Manifest
	if err := json.Unmarshal(msg.Payload, &manifest); err != nil {
		return nil, err
	}
	return &manifest, nil
}

type ProgressReader struct {
	Reader     io.Reader
	Total      int64
	Current    int64
	OnProgress func(current, total int64)
}

func (pr *ProgressReader) Read(p []byte) (int, error) {
	n, err := pr.Reader.Read(p)
	if n > 0 {
		pr.Current += int64(n)
		if pr.OnProgress != nil {
			pr.OnProgress(pr.Current, pr.Total)
		}
	}
	return n, err
}

type ProgressWriter struct {
	Writer     io.Writer
	Total      int64
	Current    int64
	OnProgress func(current, total int64)
}

func (pw *ProgressWriter) Write(p []byte) (int, error) {
	n, err := pw.Writer.Write(p)
	if n > 0 {
		pw.Current += int64(n)
		if pw.OnProgress != nil {
			pw.OnProgress(pw.Current, pw.Total)
		}
	}
	return n, err
}

func calculateHashAndBlocks(path string) (string, []string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", nil, err
	}
	defer file.Close()

	hash := blake3.New(32, nil)
	var blockHashes []string

	buffer := make([]byte, BlockSize)
	for {
		n, err := file.Read(buffer)
		if n > 0 {
			hash.Write(buffer[:n])

			blockSum := blake3.Sum256(buffer[:n])
			blockHashes = append(blockHashes, hex.EncodeToString(blockSum[:]))
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", nil, err
		}
	}

	return hex.EncodeToString(hash.Sum(nil)), blockHashes, nil
}

func SetStreamDeadline(r io.Reader, d time.Duration) {
	if c, ok := r.(interface{ SetReadDeadline(time.Time) error }); ok {
		c.SetReadDeadline(time.Now().Add(d))
	}
}

type TimeoutReader struct {
	R       io.Reader
	Timeout time.Duration
}

func (t *TimeoutReader) Read(p []byte) (n int, err error) {
	if c, ok := t.R.(interface{ SetReadDeadline(time.Time) error }); ok {
		c.SetReadDeadline(time.Now().Add(t.Timeout))
	}
	return t.R.Read(p)
}

type TimeoutWriter struct {
	W       io.Writer
	Timeout time.Duration
}

func (t *TimeoutWriter) Write(p []byte) (n int, err error) {
	if c, ok := t.W.(interface{ SetWriteDeadline(time.Time) error }); ok {
		c.SetWriteDeadline(time.Now().Add(t.Timeout))
	}
	return t.W.Write(p)
}

func IsRetryableError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	retryablePatterns := []string{
		"stream reset",
		"connection reset",
		"broken pipe",
		"use of closed network connection",
		"i/o timeout",
		"temporary failure",
		"connection refused",
		"no route to host",
		"network is unreachable",
	}
	for _, pattern := range retryablePatterns {
		if containsIgnoreCase(errStr, pattern) {
			return true
		}
	}
	return false
}

func containsIgnoreCase(s, substr string) bool {
	sLower := make([]byte, len(s))
	substrLower := make([]byte, len(substr))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 32
		}
		sLower[i] = c
	}
	for i := 0; i < len(substr); i++ {
		c := substr[i]
		if c >= 'A' && c <= 'Z' {
			c += 32
		}
		substrLower[i] = c
	}
	return len(sLower) >= len(substrLower) && (len(substrLower) == 0 || findSubstring(sLower, substrLower))
}

func findSubstring(s, substr []byte) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		match := true
		for j := 0; j < len(substr); j++ {
			if s[i+j] != substr[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
