package worker_test

import (
	"fmt"

	"github.com/cenkalti/rain/internal/worker"
)

type Parent struct {
	workers worker.Workers
}

func (p *Parent) Run(stopC chan struct{}) {
	c := &Child{}
	p.workers.Start(c)
	<-stopC
}

func (p *Parent) Stop() {
	p.workers.Stop()
}

type Child struct{}

func (c *Child) Run(stopC chan struct{}) {
	close(childRun)
	fmt.Println("hello from child")
	<-stopC
	fmt.Println("child is stopped")
}

var childRun = make(chan struct{})

func Example() {
	p := &Parent{}
	go p.Run(nil)
	<-childRun
	p.Stop()
	// Output:
	// hello from child
	// child is stopped
}
