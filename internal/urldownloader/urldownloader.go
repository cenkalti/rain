package urldownloader

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"

	"github.com/cenkalti/rain/internal/bufferpool"
	"github.com/cenkalti/rain/internal/piece"
)

type URLDownloader struct {
	Source string
	closeC chan struct{}
	doneC  chan struct{}
	sync.Mutex
}

type PieceResult struct {
	Downloader *URLDownloader
	Buffer     bufferpool.Buffer
	Index      uint32
	Error      error
	Done       bool
}

func New(source string) *URLDownloader {
	ud := &URLDownloader{
		Source: source,
		closeC: make(chan struct{}),
		doneC:  make(chan struct{}),
	}
	return ud
}

func (d *URLDownloader) Close() {
	close(d.closeC)
	<-d.doneC
}

type downloadJob struct {
	Filename   string
	RangeBegin int64
	Length     int64
}

func createJobs(pieces []piece.Piece, begin, end uint32) []downloadJob {
	if begin == end {
		return nil
	}
	jobs := make([]downloadJob, 0)
	lastSec := pieces[0].Data[0]
	job := downloadJob{
		Filename: lastSec.Name,
		Length:   lastSec.Length,
	}
	for i := begin; i < end; i++ {
		pi := &pieces[i]
		for j, sec := range pi.Data {
			if i == 0 && j == 0 {
				continue
			}
			if sec.Name == lastSec.Name {
				lastSec.Length += sec.Length
			} else {
				if job.Length > 0 { // do not request 0 byte files
					jobs = append(jobs, job)
				}
				job = downloadJob{
					Filename: sec.Name,
					Length:   sec.Length,
				}
			}
		}
	}
	if job.Length > 0 { // do not request 0 byte files
		jobs = append(jobs, job)
	}
	return jobs
}

func (d *URLDownloader) Run(client *http.Client, begin, end uint32, pieces []piece.Piece, multifile bool, resultC chan *PieceResult, pool *bufferpool.Pool) {
	defer close(d.doneC)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		select {
		case <-d.doneC:
		case <-d.closeC:
		}
		cancel()
	}()
	pieceIndex := begin
	var n int // position in piece
	var buf bufferpool.Buffer
	for _, job := range createJobs(pieces, begin, end) {
		u := d.getURL(job.Filename, multifile)
		println("url:", u)
		req, err := http.NewRequest(http.MethodGet, u, nil)
		if err != nil {
			panic(err)
		}
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", job.RangeBegin, job.RangeBegin+job.Length-1))
		req = req.WithContext(ctx)
		resp, err := client.Do(req)
		if err != nil {
			println("do request error")
			d.sendResult(resultC, &PieceResult{Downloader: d, Error: err})
			return
		}
		defer resp.Body.Close()
		err = checkStatus(resp)
		if err != nil {
			d.sendResult(resultC, &PieceResult{Downloader: d, Error: err})
			return
		}
		var m int64 // position in response
		for m < job.Length {
			if n == 0 {
				println("creating new piece buffer of length:", pieces[pieceIndex].Length)
				buf = pool.Get(int(pieces[pieceIndex].Length))
			}
			toPieceEnd := int64(len(buf.Data) - n)
			toResponseEnd := job.Length - m
			var readSize int64
			if toPieceEnd < toResponseEnd {
				readSize = toPieceEnd
			} else {
				readSize = toResponseEnd
			}
			// TODO set read deadline
			// TODO do not use read full, write for loop, update downloaded bytes counter
			println("read", pieceIndex, n, m, readSize)
			o, err := io.ReadFull(resp.Body, buf.Data[n:int64(n)+readSize])
			if err != nil {
				println("piece length", pieces[pieceIndex].Length)
				println("read", n, "bytes")
				println("read bytes:", string(buf.Data[:n]))
				d.sendResult(resultC, &PieceResult{Downloader: d, Error: err})
				return
			}
			n += o
			m += int64(o)
			if n == len(buf.Data) {
				done := pieceIndex == end
				d.sendResult(resultC, &PieceResult{Downloader: d, Buffer: buf, Index: pieceIndex, Done: done})
				if done {
					return
				}
				pieceIndex++
				n = 0
			}
		}
	}
}

func (d *URLDownloader) getURL(filename string, multifile bool) string {
	src := d.Source
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

func (d *URLDownloader) sendResult(resultC chan *PieceResult, res *PieceResult) {
	select {
	case resultC <- res:
	case <-d.closeC:
	}
}

func checkStatus(resp *http.Response) error {
	switch resp.StatusCode {
	case 200:
		// TODO assert content length header
	case 206:
		// TODO assert content length header
	default:
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}
	return nil
}
