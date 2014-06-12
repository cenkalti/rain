package rain

import (
	"sync"
)

type block struct{}

func newBlock() *block {
	return &block{}
}

func (b *block) download(wg *sync.WaitGroup) {
	wg.Done()
}
