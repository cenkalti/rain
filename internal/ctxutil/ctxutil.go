package ctxutil

import "context"

// FromChan returns a context that is cancelled when ch is closed, or when the
// returned cancel function is called. cancel must be called to release the
// goroutine that bridges the channel to the context.
func FromChan(ch <-chan struct{}) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		select {
		case <-ch:
			cancel()
		case <-ctx.Done():
		}
	}()
	return ctx, cancel
}
