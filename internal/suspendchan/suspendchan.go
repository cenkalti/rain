package suspendchan

// Chan is a wrapper around a channel.
// Underlying chan must be accessed with ReceiveC method.
// If the Chan is suspended, ReceiveC will return nil which makes the receiver to block on the Chan.
type Chan struct {
	ch        chan interface{}
	suspended bool
}

// New returns a new Chan.
func New(buflen int) *Chan {
	return &Chan{
		ch: make(chan interface{}, buflen),
	}
}

// SendC returns the underlying channel for sending values.
func (c *Chan) SendC() chan interface{} {
	return c.ch
}

// ReceiveC returns the underlying channel for receiving values.
// If the channel is suspended, it returns nil.
func (c *Chan) ReceiveC() chan interface{} {
	if c.suspended {
		return nil
	}
	return c.ch
}

// Suspend the channel.
func (c *Chan) Suspend() {
	c.suspended = true
}

// Resume the channel. Reverts previous suspend operation.
func (c *Chan) Resume() {
	c.suspended = false
}
