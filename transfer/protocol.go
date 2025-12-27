package transfer

import (
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// MessageType identifies the type of protocol message
type MessageType uint8

const (
	MsgManifest MessageType = iota
	MsgResume
	MsgFileStart
	// MsgFileData is deprecated/removed in favor of raw stream
	MsgFileEnd
	MsgComplete
	MsgError
	MsgHandshake
	MsgHandshakeAck
)

// Message is the base protocol message
type Message struct {
	Type    MessageType `json:"type"`
	Payload []byte      `json:"payload,omitempty"`
}

// HandshakeMsg is sent by the receiver to authenticate
type HandshakeMsg struct {
	Code string `json:"code"`
}

// HandshakeAckMsg is sent by the sender to confirm and set options
type HandshakeAckMsg struct {
	Compress bool `json:"compress"`
}

// Manifest describes the folder being transferred
type Manifest struct {
	FolderName string      `json:"folder_name"`
	TotalSize  int64       `json:"total_size"`
	Files      []FileEntry `json:"files"`
}

// FileEntry describes a single file in the manifest
type FileEntry struct {
	Path        string      `json:"path"` // Relative path within folder
	Size        int64       `json:"size"`
	Mode        os.FileMode `json:"mode"`
	Checksum    string      `json:"checksum"`
	BlockHashes []string    `json:"block_hashes,omitempty"`
}

const BlockSize = 1024 * 1024 // 1MB blocks
const MaxMessageSize = 10 << 20 // 10MB limit for JSON messages

// ResumeMsg contains the offsets for files that need to be resumed
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

// NewCompressedStream creates a new compressed stream
func NewCompressedStream(s io.ReadWriteCloser) (*CompressedStream, error) {
	w := gzip.NewWriter(s)
	// Force write header so the other side's reader doesn't block
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

func (cs *CompressedStream) Close() error {
	if err := cs.w.Close(); err != nil {
		return err
	}
	return cs.c.Close()
}

func (cs *CompressedStream) Flush() error {
	return cs.w.Flush()
}

// BuildManifest scans a folder or file and creates a manifest
func BuildManifest(path string, cache bool) (*Manifest, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("cannot access path: %w", err)
	}

	manifestFile := filepath.Join(path, ".2c1f_manifest.json")
	if cache && info.IsDir() {
		if data, err := os.ReadFile(manifestFile); err == nil {
			var cachedManifest Manifest
			if err := json.Unmarshal(data, &cachedManifest); err == nil {
				// Simple validation: check if total size matches (naive but fast)
				// A more robust check would be to re-stat everything, but that defeats the purpose of caching for speed on huge folders
				// For now, if cached manifest exists and option is enabled, we use it.
				return &cachedManifest, nil
			}
		}
	}

	manifest := &Manifest{
		FolderName: filepath.Base(path),
		Files:      []FileEntry{},
	}

	if !info.IsDir() {
		// Single file case
		hash, blockHashes, err := calculateHashAndBlocks(path)
		if err != nil {
			return nil, fmt.Errorf("failed to calculate hash: %w", err)
		}
		manifest.Files = append(manifest.Files, FileEntry{
			Path:        filepath.Base(path),
			Size:        info.Size(),
			Mode:        info.Mode(),
			Checksum:    hash,
			BlockHashes: blockHashes,
		})
		manifest.TotalSize = info.Size()
		return manifest, nil
	}

	// Directory case
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

		relPath, err := filepath.Rel(path, walkPath)
		if err != nil {
			return err
		}

		hash, blockHashes, err := calculateHashAndBlocks(walkPath)
		if err != nil {
			return err
		}

		manifest.Files = append(manifest.Files, FileEntry{
			Path:        filepath.ToSlash(relPath), // Use forward slashes for cross-platform
			Size:        info.Size(),
			Mode:        info.Mode(),
			Checksum:    hash,
			BlockHashes: blockHashes,
		})
		manifest.TotalSize += info.Size()

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to walk folder: %w", err)
	}

	if cache && info.IsDir() {
		data, err := json.MarshalIndent(manifest, "", "  ")
		if err == nil {
			os.WriteFile(manifestFile, data, 0644)
		}
	}

	return manifest, nil
}

// WriteMessage writes a protocol message to a stream
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

// ReadMessage reads a protocol message from a stream
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

// SendManifest sends the manifest to the receiver
func SendManifest(w io.Writer, manifest *Manifest) error {
	data, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	return WriteMessage(w, &Message{Type: MsgManifest, Payload: data})
}

// ParseManifest extracts manifest from a message
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

// ProgressReader wraps an io.Reader and tracks bytes read
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

// ProgressWriter wraps an io.Writer and tracks bytes written
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

	hash := sha256.New()
	var blockHashes []string
	
	buffer := make([]byte, BlockSize)
	for {
		n, err := file.Read(buffer)
		if n > 0 {
			// Update full file hash
			hash.Write(buffer[:n])

			// Calculate block hash
			blockSum := sha256.Sum256(buffer[:n])
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
