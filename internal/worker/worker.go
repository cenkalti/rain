package worker

import "sync"

type Worker interface {
	// Run is a blocking method that usually contains a for/select loop.
	Run(stopC chan struct{})
}

type Workers struct {
	stopC chan struct{}
	wg    sync.WaitGroup
}

func (w *Workers) StartWithOnFinishHandler(r Worker, onFinish func()) {
	if w.stopC == nil {
		w.stopC = make(chan struct{})
	}
	w.wg.Add(1)
	go func() {
		r.Run(w.stopC)
		if onFinish != nil {
			onFinish()
		}
		w.wg.Done()
	}()
}

func (w *Workers) Start(r Worker) {
	w.StartWithOnFinishHandler(r, nil)
}

func (w *Workers) Stop() {
	if w.stopC != nil {
		close(w.stopC)
	}
	w.wg.Wait()
}
