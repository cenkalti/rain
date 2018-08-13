package uploader

type Uploader struct {
}

func New() *Uploader {
	return &Uploader{}
}

func (u *Uploader) Run(stopC chan struct{}) {
}
