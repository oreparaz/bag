package wget

import (
	"errors"
	"net/url"
	"strings"
)

func normalizeURL(raw string) (*url.URL, error) {
	if raw == "" {
		return nil, errors.New("empty URL")
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	if u.Host == "" {
		return nil, errors.New("missing host")
	}
	return u, nil
}

func isHTTPScheme(s string) bool {
	switch strings.ToLower(s) {
	case "http", "https":
		return true
	}
	return false
}
