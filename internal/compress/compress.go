// Package compress is the shared compress/decompress layer used by bag's
// archive tools. It hides the differences between gzip, bzip2, xz, and
// zstd behind a single Format-keyed API.
//
// All four compressors are pure Go:
//
//   - gzip  via stdlib  (compress/gzip)
//   - bzip2 read via stdlib, write via dsnet/compress/bzip2
//   - xz    via ulikunitz/xz
//   - zstd  via klauspost/compress/zstd
//
// CGO_ENABLED=0 still produces a static binary.
package compress

import (
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"strings"

	stdbzip2 "compress/bzip2"

	dsbzip2 "github.com/dsnet/compress/bzip2"
	"github.com/klauspost/compress/zstd"
	"github.com/ulikunitz/xz"
)

// Format identifies one of the supported compression families.
type Format int

const (
	FormatNone Format = iota
	FormatGzip
	FormatBzip2
	FormatXZ
	FormatZstd
)

func (f Format) String() string {
	switch f {
	case FormatGzip:
		return "gzip"
	case FormatBzip2:
		return "bzip2"
	case FormatXZ:
		return "xz"
	case FormatZstd:
		return "zstd"
	}
	return "none"
}

// Extension returns the conventional filename suffix for the format.
func (f Format) Extension() string {
	switch f {
	case FormatGzip:
		return ".gz"
	case FormatBzip2:
		return ".bz2"
	case FormatXZ:
		return ".xz"
	case FormatZstd:
		return ".zst"
	}
	return ""
}

// FormatFromExt returns the format implied by a filename's suffix, or
// FormatNone if no known suffix matches.
func FormatFromExt(name string) Format {
	switch {
	case strings.HasSuffix(name, ".gz") || strings.HasSuffix(name, ".tgz"):
		return FormatGzip
	case strings.HasSuffix(name, ".bz2") || strings.HasSuffix(name, ".tbz") || strings.HasSuffix(name, ".tbz2"):
		return FormatBzip2
	case strings.HasSuffix(name, ".xz") || strings.HasSuffix(name, ".txz"):
		return FormatXZ
	case strings.HasSuffix(name, ".zst") || strings.HasSuffix(name, ".tzst"):
		return FormatZstd
	}
	return FormatNone
}

// FormatFromMagic peeks the first few bytes of r and returns the implied
// format. It returns a wrapped reader that includes the peeked bytes so
// the caller can hand it to NewReader.
func FormatFromMagic(r io.Reader) (Format, io.Reader, error) {
	const peek = 6
	buf := make([]byte, peek)
	n, err := io.ReadFull(r, buf)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return FormatNone, nil, err
	}
	buf = buf[:n]
	wrapped := io.MultiReader(bytesReader(buf), r)
	switch {
	case n >= 2 && buf[0] == 0x1f && buf[1] == 0x8b:
		return FormatGzip, wrapped, nil
	case n >= 3 && buf[0] == 'B' && buf[1] == 'Z' && buf[2] == 'h':
		return FormatBzip2, wrapped, nil
	case n >= 6 && buf[0] == 0xfd && buf[1] == '7' && buf[2] == 'z' &&
		buf[3] == 'X' && buf[4] == 'Z' && buf[5] == 0x00:
		return FormatXZ, wrapped, nil
	case n >= 4 && buf[0] == 0x28 && buf[1] == 0xb5 && buf[2] == 0x2f && buf[3] == 0xfd:
		return FormatZstd, wrapped, nil
	}
	return FormatNone, wrapped, nil
}

// NewReader returns a streaming decompressor for the given format. The
// caller is responsible for closing the returned ReadCloser.
func NewReader(f Format, r io.Reader) (io.ReadCloser, error) {
	switch f {
	case FormatGzip:
		gr, err := gzip.NewReader(r)
		if err != nil {
			return nil, err
		}
		return gr, nil
	case FormatBzip2:
		return io.NopCloser(stdbzip2.NewReader(r)), nil
	case FormatXZ:
		xr, err := xz.NewReader(r)
		if err != nil {
			return nil, err
		}
		return io.NopCloser(xr), nil
	case FormatZstd:
		zr, err := zstd.NewReader(r)
		if err != nil {
			return nil, err
		}
		return zstdCloser{zr}, nil
	case FormatNone:
		return io.NopCloser(r), nil
	}
	return nil, fmt.Errorf("unknown format: %d", f)
}

// NewWriter returns a streaming compressor for the given format and level.
// Level zero means "default". Callers must Close the returned writer to
// flush trailing bytes.
func NewWriter(f Format, w io.Writer, level int) (io.WriteCloser, error) {
	switch f {
	case FormatGzip:
		l := level
		if l == 0 {
			l = gzip.DefaultCompression
		}
		gw, err := gzip.NewWriterLevel(w, l)
		if err != nil {
			return nil, err
		}
		return gw, nil
	case FormatBzip2:
		l := level
		if l == 0 {
			l = dsbzip2.DefaultCompression
		}
		return dsbzip2.NewWriter(w, &dsbzip2.WriterConfig{Level: l})
	case FormatXZ:
		return xz.NewWriter(w)
	case FormatZstd:
		opts := []zstd.EOption{}
		if level > 0 {
			opts = append(opts, zstd.WithEncoderLevel(zstdLevelFromInt(level)))
		}
		zw, err := zstd.NewWriter(w, opts...)
		if err != nil {
			return nil, err
		}
		return zw, nil
	case FormatNone:
		return nopWriteCloser{w}, nil
	}
	return nil, fmt.Errorf("unknown format: %d", f)
}

func zstdLevelFromInt(l int) zstd.EncoderLevel {
	switch {
	case l <= 1:
		return zstd.SpeedFastest
	case l <= 3:
		return zstd.SpeedDefault
	case l <= 7:
		return zstd.SpeedBetterCompression
	default:
		return zstd.SpeedBestCompression
	}
}

type zstdCloser struct{ *zstd.Decoder }

func (z zstdCloser) Close() error {
	z.Decoder.Close()
	return nil
}

type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }

// bytesReader makes an io.Reader from a byte slice without importing bytes.
// (We avoid the import to keep this file's dep surface tight.)
func bytesReader(b []byte) io.Reader { return &sliceReader{s: b} }

type sliceReader struct {
	s []byte
	i int
}

func (r *sliceReader) Read(p []byte) (int, error) {
	if r.i >= len(r.s) {
		return 0, io.EOF
	}
	n := copy(p, r.s[r.i:])
	r.i += n
	return n, nil
}
