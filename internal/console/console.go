package console

import (
	"github.com/cenkalti/rain/rpc/rpcclient"
	"github.com/jroimartin/gocui"
)

type Console struct {
	client *rpcclient.RPCClient
}

func New(clt *rpcclient.RPCClient) *Console {
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
