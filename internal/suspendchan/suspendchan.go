package suspendchan

// Chan is a wrapper around a channel.
// Underlying chan must be accessed with ReceiveC method.
// If the Chan is suspended, ReceiveC will return nil which makes the receiver to block on the Chan.
type Chan[T any] struct {
	ch        chan T
	suspended bool
}

// New returns a new Chan.
func New[T any](buflen int) *Chan[T] {
	return &Chan[T]{
		ch: make(chan T, buflen),
	}
}

// SendC returns the underlying channel for sending values.
func (c *Chan[T]) SendC() chan T {
	return c.ch
}

// ReceiveC returns the underlying channel for receiving values.
// If the channel is suspended, it returns nil.
func (c *Chan[T]) ReceiveC() chan T {
	if c.suspended {
		return nil
	}
	return c.ch
}

// Suspend the channel.
func (c *Chan[T]) Suspend() {
	c.suspended = true
}

// Resume the channel. Reverts previous suspend operation.
func (c *Chan[T]) Resume() {
	c.suspended = false
}
