package console

import (
	"github.com/cenkalti/rain/rainrpc"
	"github.com/jroimartin/gocui"
)

type Console struct {
	client *rainrpc.Client
}

func New(clt *rainrpc.Client) *Console {
	return &Console{
		client: clt,
	}
}

func (c *Console) Run() error {
	g, err := gocui.NewGui(gocui.OutputNormal)
	if err != nil {
		// handle error
	}
	defer g.Close()

	// Set GUI managers and key bindings
	// ...

	err = g.MainLoop()
	if err == gocui.ErrQuit {
		err = nil
	}
	return err
}
