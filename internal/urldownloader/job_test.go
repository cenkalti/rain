package urldownloader

import (
	"testing"

	"github.com/cenkalti/rain/v2/internal/filesection"
	"github.com/cenkalti/rain/v2/internal/piece"
	"github.com/stretchr/testify/assert"
)

func TestCreateJobs(t *testing.T) {
	pieces := []piece.Piece{
		{
			Data: []filesection.FileSection{
				{
					Name:   "file1",
					Offset: 0,
					Length: 10,
				},
				{
					Name:   "file2",
					Offset: 0,
					Length: 5,
				},
				{
					Name:   "file3",
					Offset: 0,
					Length: 1,
				},
			},
		},
		{
			Data: []filesection.FileSection{
				{
					Name:   "file3",
					Offset: 1,
					Length: 16,
				},
			},
		},
		{
			Data: []filesection.FileSection{
				{
					Name:   "file3",
					Offset: 17,
					Length: 2,
				},
				{
					Name:   "file4",
					Offset: 0,
					Length: 8,
				},
			},
		},
	}
	assert.Equal(t, []downloadJob{
		{
			Filename:   "file1",
			RangeBegin: 0,
			Length:     10,
		},
		{
			Filename:   "file2",
			RangeBegin: 0,
			Length:     5,
		},
		{
			Filename:   "file3",
			RangeBegin: 0,
			Length:     19,
		},
		{
			Filename:   "file4",
			RangeBegin: 0,
			Length:     8,
		},
	}, createJobs(pieces, 0, 3))
	assert.Equal(t, []downloadJob{
		{
			Filename:   "file1",
			RangeBegin: 0,
			Length:     10,
		},
		{
			Filename:   "file2",
			RangeBegin: 0,
			Length:     5,
		},
		{
			Filename:   "file3",
			RangeBegin: 0,
			Length:     1,
		},
	}, createJobs(pieces, 0, 1))
	assert.Equal(t, []downloadJob{
		{
			Filename:   "file3",
			RangeBegin: 1,
			Length:     16,
		},
	}, createJobs(pieces, 1, 2))
	assert.Equal(t, []downloadJob{
		{
			Filename:   "file3",
			RangeBegin: 17,
			Length:     2,
		},
		{
			Filename:   "file4",
			RangeBegin: 0,
			Length:     8,
		},
	}, createJobs(pieces, 2, 3))
	assert.Equal(t, ([]downloadJob)(nil), createJobs(pieces, 2, 2))
}

func TestCreateJobsPadding(t *testing.T) {
	// Piece length is 16. fileA and fileB are aligned to piece boundaries
	// with padding files (BEP 47). fileB spans pieces 1 and 2.
	pieces := []piece.Piece{
		{
			Data: []filesection.FileSection{
				{Name: "fileA", Offset: 0, Length: 10},
				{Name: ".pad/6", Offset: 0, Length: 6, Padding: true},
			},
		},
		{
			Data: []filesection.FileSection{
				{Name: "fileB", Offset: 0, Length: 16},
			},
		},
		{
			Data: []filesection.FileSection{
				{Name: "fileB", Offset: 16, Length: 4},
				{Name: ".pad/12", Offset: 0, Length: 12, Padding: true},
			},
		},
		{
			Data: []filesection.FileSection{
				{Name: "fileC", Offset: 0, Length: 4},
			},
		},
	}
	assert.Equal(t, []downloadJob{
		{Filename: "fileA", RangeBegin: 0, Length: 10},
		{Filename: ".pad/6", RangeBegin: 0, Length: 6, Padding: true},
		{Filename: "fileB", RangeBegin: 0, Length: 20},
		{Filename: ".pad/12", RangeBegin: 0, Length: 12, Padding: true},
		{Filename: "fileC", RangeBegin: 0, Length: 4},
	}, createJobs(pieces, 0, 4))
	assert.Equal(t, []downloadJob{
		{Filename: "fileB", RangeBegin: 16, Length: 4},
		{Filename: ".pad/12", RangeBegin: 0, Length: 12, Padding: true},
		{Filename: "fileC", RangeBegin: 0, Length: 4},
	}, createJobs(pieces, 2, 4))
}
