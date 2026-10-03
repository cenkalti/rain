package console

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/cenkalti/rain/v2/internal/rpctypes"
)

func columnsNeedStats(columns []string) bool {
	l := []string{"ID", "Name", "InfoHash", "Port"}
	for _, c := range columns {
		for _, d := range l {
			if c != d {
				return true
			}
		}
	}
	return false
}

func isURI(arg string) bool {
	return strings.HasPrefix(arg, "magnet:") || strings.HasPrefix(arg, "http://") || strings.HasPrefix(arg, "https://")
}

func flags(p rpctypes.Peer) string {
	var sb strings.Builder
	sb.Grow(6)
	if p.ClientInterested {
		if p.PeerChoking {
			sb.WriteString("d")
		} else {
			sb.WriteString("D")
		}
	} else {
		if !p.PeerChoking {
			sb.WriteString("K")
		} else {
			sb.WriteString(" ")
		}
	}
	if p.PeerInterested {
		if p.ClientChoking {
			sb.WriteString("u")
		} else {
			sb.WriteString("U")
		}
	} else {
		if !p.ClientChoking {
			sb.WriteString("?")
		} else {
			sb.WriteString(" ")
		}
	}
	if p.OptimisticUnchoked {
		sb.WriteString("O")
	} else {
		sb.WriteString(" ")
	}
	if p.Snubbed {
		sb.WriteString("S")
	} else {
		sb.WriteString(" ")
	}
	switch p.Source {
	case "DHT":
		sb.WriteString("H")
	case "PEX":
		sb.WriteString("X")
	case "INCOMING":
		sb.WriteString("I")
	case "MANUAL":
		sb.WriteString("M")
	default:
		sb.WriteString(" ")
	}
	switch {
	case p.EncryptedStream:
		sb.WriteString("E")
	case p.EncryptedHandshake:
		sb.WriteString("e")
	default:
		sb.WriteString(" ")
	}
	return sb.String()
}

func getProgress(stats *rpctypes.Stats) int {
	var progress int
	if stats.Pieces.Total > 0 {
		switch stats.Status {
		case "Verifying":
			progress = int(stats.Pieces.Checked * 100 / stats.Pieces.Total)
		case "Allocating":
			progress = int(stats.Bytes.Allocated * 100 / stats.Bytes.Total)
		default:
			progress = int(stats.Pieces.Have * 100 / stats.Pieces.Total)
		}
	}
	return progress
}

func getRatio(stats *rpctypes.Stats) float64 {
	var ratio float64
	if stats.Bytes.Downloaded > 0 {
		ratio = float64(stats.Bytes.Uploaded) / float64(stats.Bytes.Downloaded)
	}
	return ratio
}

func getSize(stats *rpctypes.Stats) string {
	var size string
	switch {
	case stats.Bytes.Total < 1<<10:
		size = fmt.Sprintf("%d bytes", stats.Bytes.Total)
	case stats.Bytes.Total < 1<<20:
		size = fmt.Sprintf("%d KiB", stats.Bytes.Total/(1<<10))
	default:
		size = fmt.Sprintf("%d MiB", stats.Bytes.Total/(1<<20))
	}
	return size
}

func getDownloadSpeed(stats *rpctypes.Stats) string {
	return fmt.Sprintf("%d KiB/s", stats.Speed.Download/1024)
}

func getUploadSpeed(stats *rpctypes.Stats) string {
	return fmt.Sprintf("%d KiB/s", stats.Speed.Upload/1024)
}

func getETA(stats *rpctypes.Stats) string {
	var eta string
	if stats.ETA != -1 {
		eta = (time.Duration(stats.ETA) * time.Second).String()
	}
	return eta
}

// FormatStats returns the human readable representation of torrent stats object.
func FormatStats(stats *rpctypes.Stats, v io.Writer) {
	fmt.Fprintf(v, "Name: %s\n", stats.Name)
	fmt.Fprintf(v, "Private: %v\n", stats.Private)
	status := stats.Status
	if status == "Stopped" && stats.Error != "" {
		status = status + ": " + stats.Error
	}
	fmt.Fprintf(v, "Status: %s\n", status)
	fmt.Fprintf(v, "Progress: %d%%\n", getProgress(stats))
	fmt.Fprintf(v, "Ratio: %.2f\n", getRatio(stats))
	fmt.Fprintf(v, "Size: %s\n", getSize(stats))
	fmt.Fprintf(v, "Peers: %d in / %d out\n", stats.Peers.Incoming, stats.Peers.Outgoing)
	fmt.Fprintf(v, "Download speed: %11s\n", getDownloadSpeed(stats))
	fmt.Fprintf(v, "Upload speed:   %11s\n", getUploadSpeed(stats))
	fmt.Fprintf(v, "ETA: %s\n", getETA(stats))
}

// FormatSessionStats returns the human readable representation of session stats object.
func FormatSessionStats(s *rpctypes.SessionStats, v io.Writer) {
	fmt.Fprintf(v, "Torrents: %d, Peers: %d, Uptime: %s\n", s.Torrents, s.Peers, time.Duration(s.Uptime)*time.Second)
	fmt.Fprintf(v, "BlocklistRules: %d, Updated: %s ago\n", s.BlockListRules, time.Duration(s.BlockListRecency)*time.Second)
	fmt.Fprintf(v, "Reads: %d/s, %dKB/s, Active: %d, Pending: %d\n", s.ReadsPerSecond, s.SpeedRead/1024, s.ReadsActive, s.ReadsPending)
	fmt.Fprintf(v, "Writes: %d/s, %dKB/s, Active: %d, Pending: %d\n", s.WritesPerSecond, s.SpeedWrite/1024, s.WritesActive, s.WritesPending)
	fmt.Fprintf(v, "ReadCache Objects: %d, Size: %dMB, Utilization: %d%%\n", s.ReadCacheObjects, s.ReadCacheSize/(1<<20), s.ReadCacheUtilization)
	fmt.Fprintf(v, "WriteCache Objects: %d, Size: %dMB, PendingKeys: %d\n", s.WriteCacheObjects, s.WriteCacheSize/(1<<20), s.WriteCachePendingKeys)
	fmt.Fprintf(v, "DownloadSpeed: %dKB/s, UploadSpeed: %dKB/s\n", s.SpeedDownload/1024, s.SpeedUpload/1024)
	fmt.Fprintf(v, "BytesDownloaded: %dMB, BytesUploaded: %dMB\n", s.BytesDownloaded/1024/1024, s.BytesUploaded/1024/1024)
	fmt.Fprintf(v, "BytesRead: %dMB, BytesWritten: %dMB\n", s.BytesRead/1024/1024, s.BytesWritten/1024/1024)
}
