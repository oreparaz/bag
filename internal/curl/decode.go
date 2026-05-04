package curl

import (
	"compress/flate"
	"compress/gzip"
	"io"
	"net/http"
	"strings"
)

// decodeBody returns a reader that decompresses the response body if
// --compressed is set and the server actually returned a compressed
// payload.
//
// net/http's Transport handles gzip transparently when no Accept-Encoding
// header is set by the caller. Since we always set Accept-Encoding when
// --compressed is passed, decompression is left to us.
func decodeBody(resp *http.Response, compressed bool) io.Reader {
	if !compressed {
		return resp.Body
	}
	enc := strings.ToLower(resp.Header.Get("Content-Encoding"))
	switch enc {
	case "gzip":
		r, err := gzip.NewReader(resp.Body)
		if err != nil {
			return resp.Body
		}
		// Transparent: strip Content-Encoding from response so writeStatusAndHeaders is unchanged.
		resp.Header.Del("Content-Encoding")
		return r
	case "deflate":
		resp.Header.Del("Content-Encoding")
		return flate.NewReader(resp.Body)
	}
	return resp.Body
}
