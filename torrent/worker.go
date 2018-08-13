package torrent

import "sync"

type worker interface {
	Run(stopC chan struct{})
}

type workers []worker

func (s workers) start(stopC chan struct{}, stopWG *sync.WaitGroup) {
	var wg sync.WaitGroup
	for _, w := range s {
		wg.Add(1)
		go func(w worker) {
			w.Run(stopC)
			wg.Done()
		}(w)
	}
}
