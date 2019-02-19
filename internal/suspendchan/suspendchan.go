package suspendchan

type Chan struct {
	ch        chan interface{}
	suspended bool
}

func New(buflen int) *Chan {
	return &Chan{
		ch: make(chan interface{}, buflen),
	}
}

func (c *Chan) SendC() chan interface{} {
	return c.ch
}

func (c *Chan) ReceiveC() chan interface{} {
	if c.suspended {
		return nil
	}
	return c.ch
}

func (c *Chan) Suspend() {
	c.suspended = true
}

func (c *Chan) Resume() {
	c.suspended = false
}
