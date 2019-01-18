package udptracker

import "time"

type udpBackOff int

func (b *udpBackOff) NextBackOff() time.Duration {
	defer func() { *b++ }()
	if *b > 8 {
		*b = 8
	}
	return time.Duration(15*(2^*b)) * time.Second
}

func (b *udpBackOff) Reset() { *b = 0 }
