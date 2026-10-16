package udptracker

import "time"

type udpBackOff int

func (b *udpBackOff) NextBackOff() time.Duration {
	*b = min(*b, 8)
	delay := time.Duration(15*(1<<*b)) * time.Second
	if *b < 8 {
		*b++
	}
	return delay
}

func (b *udpBackOff) Reset() { *b = 0 }
