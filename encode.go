package demolens

import (
	"compress/gzip"
	"encoding/json"
	"io"

	"github.com/andybalholm/brotli"
	"github.com/f-gillmann/demolens/v2/model"
)

// WriteJSON encodes the match as JSON to w. minify omits indentation (compact);
// otherwise it is indented with two spaces. The model's custom marshalers
// (2dp positions, multi_kills array, steam-id strings) are applied either way.
func WriteJSON(w io.Writer, m *model.Match, minify bool) error {
	enc := json.NewEncoder(w)
	if !minify {
		enc.SetIndent("", "  ")
	}
	return enc.Encode(m)
}

// WriteGzJSON gzip-compresses the JSON encoding of the match to w. minify is
// passed through to WriteJSON.
func WriteGzJSON(w io.Writer, m *model.Match, minify bool) (err error) {
	gw := gzip.NewWriter(w)
	defer func() {
		if cerr := gw.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()
	return WriteJSON(gw, m, minify)
}

// WriteBrJSON brotli-compresses the JSON encoding of the match to w, at brotli's
// default quality level. minify is passed through to WriteJSON.
func WriteBrJSON(w io.Writer, m *model.Match, minify bool) (err error) {
	bw := brotli.NewWriter(w)
	defer func() {
		if cerr := bw.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()
	return WriteJSON(bw, m, minify)
}
