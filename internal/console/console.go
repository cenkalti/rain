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
		return err
	}
	defer g.Close()

	m := new(Manager)
	g.SetManager(m)

	g.SetKeybinding("", gocui.KeyCtrlC, gocui.ModNone, quit)
	g.SetKeybinding("", 'q', gocui.ModNone, quit)

	err = g.MainLoop()
	if err == gocui.ErrQuit {
		err = nil
	}
	return err
}

func quit(g *gocui.Gui, v *gocui.View) error {
	return gocui.ErrQuit
}
