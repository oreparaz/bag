package curl

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// buildBody returns method, body, content-type for the request.
//
// Method defaults: GET if no body, POST when -d/-F is present, unless
// the user set --request explicitly.
func (a *app) buildBody() (string, io.Reader, string, error) {
	o := a.opts

	// Handle -F multipart.
	if o.hasForm {
		buf := &bytes.Buffer{}
		mw := multipart.NewWriter(buf)
		for _, f := range o.Forms {
			if err := writeFormPart(mw, f); err != nil {
				_ = mw.Close()
				return "", nil, "", err
			}
		}
		if err := mw.Close(); err != nil {
			return "", nil, "", err
		}
		method := o.Method
		if method == "" {
			method = "POST"
		}
		return method, buf, mw.FormDataContentType(), nil
	}

	// Handle -d / --data*.
	if o.hasData {
		body, ct, err := encodeData(o.Data)
		if err != nil {
			return "", nil, "", err
		}
		method := o.Method
		if method == "" {
			method = "POST"
		}
		return method, body, ct, nil
	}

	method := o.Method
	if method == "" {
		method = "GET"
	}
	return method, nil, "", nil
}

func writeFormPart(mw *multipart.Writer, f formField) error {
	v := f.value
	if strings.HasPrefix(v, "@") || strings.HasPrefix(v, "<") {
		op := v[0]
		path := v[1:]
		if path == "" {
			return fmt.Errorf("empty file path in -F %q", f.name)
		}
		fh, err := os.Open(path)
		if err != nil {
			return err
		}
		defer fh.Close()
		var w io.Writer
		if op == '@' {
			w, err = mw.CreateFormFile(f.name, filepath.Base(path))
		} else {
			w, err = mw.CreateFormField(f.name)
		}
		if err != nil {
			return err
		}
		_, err = io.Copy(w, fh)
		return err
	}
	w, err := mw.CreateFormField(f.name)
	if err != nil {
		return err
	}
	_, err = io.Writer(w).Write([]byte(v))
	return err
}

// encodeData joins -d chunks with '&' (curl behavior). Newlines stripped
// from @file content for dataASCII; preserved for dataBinary; URL-encoded
// for dataURLEncode.
func encodeData(chunks []dataChunk) (io.Reader, string, error) {
	var parts [][]byte
	for _, c := range chunks {
		b, err := encodeChunk(c)
		if err != nil {
			return nil, "", err
		}
		parts = append(parts, b)
	}
	body := bytes.Join(parts, []byte("&"))
	return bytes.NewReader(body), "application/x-www-form-urlencoded", nil
}

func encodeChunk(c dataChunk) ([]byte, error) {
	switch c.kind {
	case dataRaw:
		return []byte(c.value), nil
	case dataASCII:
		if strings.HasPrefix(c.value, "@") {
			data, err := readDataFile(c.value[1:])
			if err != nil {
				return nil, err
			}
			// strip CR and LF, matching curl's -d behavior.
			data = bytes.ReplaceAll(data, []byte("\r"), nil)
			data = bytes.ReplaceAll(data, []byte("\n"), nil)
			return data, nil
		}
		return []byte(c.value), nil
	case dataBinary:
		if strings.HasPrefix(c.value, "@") {
			return readDataFile(c.value[1:])
		}
		return []byte(c.value), nil
	case dataURLEncode:
		return urlEncodeChunk(c.value)
	}
	return nil, fmt.Errorf("unhandled data chunk kind %d", c.kind)
}

// urlEncodeChunk handles --data-urlencode "name=value", "=value", "value",
// "name@file", "@file".
func urlEncodeChunk(v string) ([]byte, error) {
	name := ""
	val := v
	at := strings.IndexByte(v, '@')
	eq := strings.IndexByte(v, '=')
	switch {
	case at >= 0 && (eq < 0 || at < eq):
		name = v[:at]
		data, err := readDataFile(v[at+1:])
		if err != nil {
			return nil, err
		}
		val = string(data)
	case eq >= 0:
		name = v[:eq]
		val = v[eq+1:]
	}
	enc := url.QueryEscape(val)
	if name == "" {
		return []byte(enc), nil
	}
	return []byte(name + "=" + enc), nil
}

func readDataFile(path string) ([]byte, error) {
	if path == "-" {
		return io.ReadAll(os.Stdin)
	}
	return os.ReadFile(path)
}

func buildQueryFromData(chunks []dataChunk) (string, error) {
	var parts []string
	for _, c := range chunks {
		b, err := encodeChunk(c)
		if err != nil {
			return "", err
		}
		parts = append(parts, string(b))
	}
	return strings.Join(parts, "&"), nil
}
