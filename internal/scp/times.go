package scp

import (
	"os"
	"time"
)

// setTimes is split out so a future windows port can stub it. On Unix
// we just call os.Chtimes.
func setTimes(path string, t timesPair) error {
	mt := time.Unix(t.mtime, 0)
	at := time.Unix(t.atime, 0)
	return os.Chtimes(path, at, mt)
}
