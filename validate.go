package demolens

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"strings"

	"github.com/f-gillmann/demolens/v2/internal/parser"
)

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
	if format != parser.CS2Magic {
		return Info{}, fmt.Errorf("not a CS2 demo (header %q, want %q)", format, parser.CS2Magic)
	}

	hash := sha256.New()
	hash.Write(header)
	if _, err := io.Copy(hash, r); err != nil {
		return Info{}, fmt.Errorf("hash demo: %w", err)
	}
	return Info{Format: format, FileHash: hex.EncodeToString(hash.Sum(nil))}, nil
}
