package urldownloader

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/cenkalti/rain/internal/bufferpool"
	"github.com/cenkalti/rain/internal/piece"
)

type URLDownloader struct {
	URL                 string
	Begin, End, current uint32
	closeC, doneC       chan struct{}
}

type PieceResult struct {
	Downloader *URLDownloader
	Buffer     bufferpool.Buffer
	Index      uint32
	Error      error
	Done       bool
}

func New(source string, begin, end uint32) *URLDownloader {
	return &URLDownloader{
		URL:     source,
		Begin:   begin,
		current: begin,
		End:     end,
		closeC:  make(chan struct{}),
		doneC:   make(chan struct{}),
	}
}

func (d *URLDownloader) Close() {
	close(d.closeC)
	<-d.doneC
}

func (d *URLDownloader) String() string {
	return d.URL
}

func (d *URLDownloader) UpdateEnd(value uint32) {
	atomic.StoreUint32(&d.End, value)
}

func (d *URLDownloader) readEnd() uint32 {
	return atomic.LoadUint32(&d.End)
}

func (d *URLDownloader) incrCurrent() uint32 {
	return atomic.AddUint32(&d.current, 1)
}

func (d *URLDownloader) ReadCurrent() uint32 {
	return atomic.LoadUint32(&d.current)
}

func (d *URLDownloader) Run(client *http.Client, pieces []piece.Piece, multifile bool, resultC chan interface{}, pool *bufferpool.Pool, readTimeout time.Duration) {
	defer close(d.doneC)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		select {
		case <-d.doneC:
		case <-d.closeC:
		}
		cancel()
	}()

	jobs := createJobs(pieces, d.Begin, d.readEnd())

	var n int // position in piece
	buf := pool.Get(int(pieces[d.current].Length))

	processJob := func(job downloadJob) bool {
		u := d.getURL(job.Filename, multifile)
		req, err := http.NewRequest(http.MethodGet, u, nil)
		if err != nil {
			panic(err)
		}
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", job.RangeBegin, job.RangeBegin+job.Length-1))
		req = req.WithContext(ctx)
		resp, err := client.Do(req)
		if err != nil {
			d.sendResult(resultC, &PieceResult{Downloader: d, Error: err})
			return false
		}
		defer resp.Body.Close()
		err = checkStatus(resp)
		if err != nil {
			d.sendResult(resultC, &PieceResult{Downloader: d, Error: err})
			return false
		}
		timer := time.AfterFunc(readTimeout, cancel)
		defer timer.Stop()
		var m int64 // position in response
		for m < job.Length {
			readSize := calcReadSize(buf, n, job, m)
			o, err := readFull(resp.Body, buf.Data[n:int64(n)+readSize], timer, readTimeout)
			if err != nil {
				d.sendResult(resultC, &PieceResult{Downloader: d, Error: err})
				return false
			}
			n += o
			m += int64(o)
			if n == len(buf.Data) { // piece completed
				index := d.current
				done := d.current >= d.readEnd()-1
				d.sendResult(resultC, &PieceResult{Downloader: d, Buffer: buf, Index: index, Done: done})
				if done {
					return true
				}
				d.incrCurrent()
				// Allocate new buffer for next piece
				n = 0
				buf = pool.Get(int(pieces[d.current].Length))
			}
		}
		return true
	}
	for _, job := range jobs {
		ok := processJob(job)
		if !ok {
			break
		}
	}
}

func calcReadSize(buf bufferpool.Buffer, bufPos int, job downloadJob, jobPos int64) int64 {
	toPieceEnd := int64(len(buf.Data) - bufPos)
	toResponseEnd := job.Length - jobPos
	if toPieceEnd < toResponseEnd {
		return toPieceEnd
	}
	return toResponseEnd
}

// readFull is similar to io.ReadFull call, plus it resets the read timer on each iteration.
func readFull(r io.Reader, b []byte, t *time.Timer, d time.Duration) (o int, err error) {
	for o < len(b) && err == nil {
		var nn int
		nn, err = r.Read(b[o:])
		o += nn
		t.Reset(d)
	}
	if o >= len(b) {
		err = nil
	}
	return
}

func (d *URLDownloader) getURL(filename string, multifile bool) string {
	src := d.URL
	if !multifile {
		if src[len(src)-1] == '/' {
			src += filename
		}
		return src
	}
	if src[len(src)-1] != '/' {
		src += "/"
	}
	return src + filename
}

func (d *URLDownloader) sendResult(resultC chan interface{}, res *PieceResult) {
	select {
	case <-d.closeC:
		return
	default:
	}
	select {
	case resultC <- res:
	case <-d.closeC:
	}
}

func checkStatus(resp *http.Response) error {
	switch resp.StatusCode {
	case 200, 206:
		return nil
	default:
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}
}
