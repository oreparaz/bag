package conformance

import (
	"context"
	"time"
)

func newTimeout() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 30*time.Second)
}
