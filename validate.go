package demolens

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
)

// every CS2 demo starts with this stamp.
const cs2Magic = "PBDEMS2"

type Info struct {
	Format   string
	FileHash string
}

// Validate checks the demo header and returns the format stamp plus a SHA-256 of
// the bytes.
func Validate(r io.Reader) (Info, error) {
	header := make([]byte, 8)
	if _, err := io.ReadFull(r, header); err != nil {
		return Info{}, fmt.Errorf("read demo header: %w", err)
	}
	format := strings.TrimRight(string(header), "\x00")
	if format != cs2Magic {
		return Info{}, fmt.Errorf("not a CS2 demo (header %q, want %q)", format, cs2Magic)
	}

	hash := sha256.New()
	hash.Write(header)
	if _, err := io.Copy(hash, r); err != nil {
		return Info{}, fmt.Errorf("hash demo: %w", err)
	}
	return Info{Format: format, FileHash: hex.EncodeToString(hash.Sum(nil))}, nil
}
