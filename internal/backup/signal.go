package backup

import (
	"context"
	"os/signal"
	"syscall"
)

// SignalContext returns a context that is cancelled on SIGINT or SIGTERM.
func SignalContext(parent context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := signal.NotifyContext(parent, syscall.SIGINT, syscall.SIGTERM)
	return ctx, cancel
}
