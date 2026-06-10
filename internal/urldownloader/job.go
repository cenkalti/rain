package urldownloader

import (
	"github.com/cenkalti/rain/v2/internal/piece"
)

type downloadJob struct {
	Filename   string
	RangeBegin int64
	Length     int64
	// Padding jobs are not requested from the server because padding files
	// do not exist there. Their bytes are always zero and are skipped over
	// in the piece buffer instead.
	Padding bool
}

func createJobs(pieces []piece.Piece, begin, end uint32) []downloadJob {
	if begin == end {
		return nil
	}
	jobs := make([]downloadJob, 0)
	var job downloadJob
	for i := begin; i < end; i++ {
		pi := &pieces[i]
		for j, sec := range pi.Data {
			if i == 0 && j == 0 {
				job = downloadJob{
					Filename:   sec.Name,
					RangeBegin: sec.Offset,
					Length:     sec.Length,
					Padding:    sec.Padding,
				}
				continue
			}
			if sec.Name == job.Filename && sec.Padding == job.Padding {
				job.Length += sec.Length
				continue
			}
			if job.Length > 0 { // do not request 0 byte files
				jobs = append(jobs, job)
			}
			job = downloadJob{
				Filename:   sec.Name,
				RangeBegin: sec.Offset,
				Length:     sec.Length,
				Padding:    sec.Padding,
			}
		}
	}
	if job.Length > 0 { // do not request 0 byte files
		jobs = append(jobs, job)
	}
	return jobs
}
