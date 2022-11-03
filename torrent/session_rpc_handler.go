package torrent

import (
	"archive/tar"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cenkalti/rain/internal/resumer/boltdbresumer"
	"github.com/cenkalti/rain/internal/rpctypes"
	"github.com/powerman/rpc-codec/jsonrpc2"
)

var errTorrentNotFound = jsonrpc2.NewError(1, "torrent not found")

type rpcHandler struct {
	session *Session
}

func (h *rpcHandler) Version(args struct{}, reply *string) error {
	*reply = Version
	return nil
}

func (h *rpcHandler) ListTorrents(args *rpctypes.ListTorrentsRequest, reply *rpctypes.ListTorrentsResponse) error {
	torrents := h.session.ListTorrents()
	reply.Torrents = make([]rpctypes.Torrent, 0, len(torrents))
	for _, t := range torrents {
		reply.Torrents = append(reply.Torrents, newTorrent(t))
	}
	return nil
}

func (h *rpcHandler) AddTorrent(args *rpctypes.AddTorrentRequest, reply *rpctypes.AddTorrentResponse) error {
	r := base64.NewDecoder(base64.StdEncoding, strings.NewReader(args.Torrent))
	opt := &AddTorrentOptions{
		Stopped:           args.AddTorrentOptions.Stopped,
		ID:                args.AddTorrentOptions.ID,
		StopAfterDownload: args.StopAfterDownload,
		StopAfterMetadata: args.StopAfterMetadata,
	}
	t, err := h.session.AddTorrent(r, opt)
	var e *InputError
	if errors.As(err, &e) {
		return jsonrpc2.NewError(2, e.Error())
	}
	if err != nil {
		return err
	}
	reply.Torrent = newTorrent(t)
	return nil
}

func (h *rpcHandler) AddURI(args *rpctypes.AddURIRequest, reply *rpctypes.AddURIResponse) error {
	opt := &AddTorrentOptions{
		Stopped:           args.AddTorrentOptions.Stopped,
		ID:                args.AddTorrentOptions.ID,
		StopAfterDownload: args.StopAfterDownload,
		StopAfterMetadata: args.StopAfterMetadata,
	}
	t, err := h.session.AddURI(args.URI, opt)
	var e *InputError
	if errors.As(err, &e) {
		return jsonrpc2.NewError(2, e.Error())
	}
	if err != nil {
		return err
	}
	reply.Torrent = newTorrent(t)
	return nil
}

func newTorrent(t *Torrent) rpctypes.Torrent {
	return rpctypes.Torrent{
		ID:       t.ID(),
		Name:     t.Name(),
		InfoHash: t.InfoHash().String(),
		Port:     t.Port(),
		AddedAt:  rpctypes.Time{Time: t.AddedAt()},
	}
}

func (h *rpcHandler) RemoveTorrent(args *rpctypes.RemoveTorrentRequest, reply *rpctypes.RemoveTorrentResponse) error {
	return h.session.RemoveTorrent(args.ID)
}

func (h *rpcHandler) GetMagnet(args *rpctypes.GetMagnetRequest, reply *rpctypes.GetMagnetResponse) error {
	t := h.session.GetTorrent(args.ID)
	if t == nil {
		return errTorrentNotFound
	}
	var err error
	reply.Magnet, err = t.Magnet()
	return err
}

func (h *rpcHandler) GetTorrent(args *rpctypes.GetTorrentRequest, reply *rpctypes.GetTorrentResponse) error {
	t := h.session.GetTorrent(args.ID)
	if t == nil {
		return errTorrentNotFound
	}
	b, err := t.Torrent()
	if err != nil {
		return err
	}
	reply.Torrent = base64.StdEncoding.EncodeToString(b)
	return nil
}

func (h *rpcHandler) CleanDatabase(args *rpctypes.CleanDatabaseRequest, reply *rpctypes.CleanDatabaseResponse) error {
	return h.session.CleanDatabase()
}

func (h *rpcHandler) GetSessionStats(args *rpctypes.GetSessionStatsRequest, reply *rpctypes.GetSessionStatsResponse) error {
	s := h.session.Stats()
	reply.Stats = rpctypes.SessionStats{
		Uptime:         int(s.Uptime / time.Second),
		Torrents:       s.Torrents,
		Peers:          s.Peers,
		PortsAvailable: s.PortsAvailable,

		BlockListRules:   s.BlockListRules,
		BlockListRecency: int(s.BlockListRecency / time.Second),

		ReadCacheObjects:     s.ReadCacheObjects,
		ReadCacheSize:        s.ReadCacheSize,
		ReadCacheUtilization: s.ReadCacheUtilization,

		ReadsPerSecond: s.ReadsPerSecond,
		ReadsActive:    s.ReadsActive,
		ReadsPending:   s.ReadsPending,

		WriteCacheObjects:     s.WriteCacheObjects,
		WriteCacheSize:        s.WriteCacheSize,
		WriteCachePendingKeys: s.WriteCachePendingKeys,

		WritesPerSecond: s.WritesPerSecond,
		WritesActive:    s.WritesActive,
		WritesPending:   s.WritesPending,

		SpeedDownload: s.SpeedDownload,
		SpeedUpload:   s.SpeedUpload,
		SpeedRead:     s.SpeedRead,
		SpeedWrite:    s.SpeedWrite,

		BytesDownloaded: s.BytesDownloaded,
		BytesUploaded:   s.BytesUploaded,
		BytesRead:       s.BytesRead,
		BytesWritten:    s.BytesWritten,
	}
	return nil
}

func (h *rpcHandler) GetTorrentStats(args *rpctypes.GetTorrentStatsRequest, reply *rpctypes.GetTorrentStatsResponse) error {
	t := h.session.GetTorrent(args.ID)
	if t == nil {
		return errTorrentNotFound
	}
	s := t.Stats()
	reply.Stats = rpctypes.Stats{
		InfoHash: s.InfoHash.String(),
		Port:     s.Port,
		Status:   s.Status.String(),
		Pieces: struct {
			Checked   uint32
			Have      uint32
			Missing   uint32
			Available uint32
			Total     uint32
		}{
			Checked:   s.Pieces.Checked,
			Have:      s.Pieces.Have,
			Missing:   s.Pieces.Missing,
			Available: s.Pieces.Available,
			Total:     s.Pieces.Total,
		},
		Bytes: struct {
			Total      int64
			Allocated  int64
			Completed  int64
			Incomplete int64
			Downloaded int64
			Uploaded   int64
			Wasted     int64
		}{
			Total:      s.Bytes.Total,
			Allocated:  s.Bytes.Allocated,
			Completed:  s.Bytes.Completed,
			Incomplete: s.Bytes.Incomplete,
			Downloaded: s.Bytes.Downloaded,
			Uploaded:   s.Bytes.Uploaded,
			Wasted:     s.Bytes.Wasted,
		},
		Peers: struct {
			Total    int
			Incoming int
			Outgoing int
		}{
			Total:    s.Peers.Total,
			Incoming: s.Peers.Incoming,
			Outgoing: s.Peers.Outgoing,
		},
		Handshakes: struct {
			Total    int
			Incoming int
			Outgoing int
		}{
			Total:    s.Handshakes.Total,
			Incoming: s.Handshakes.Incoming,
			Outgoing: s.Handshakes.Outgoing,
		},
		Addresses: struct {
			Total   int
			Tracker int
			DHT     int
			PEX     int
		}{
			Total:   s.Addresses.Total,
			Tracker: s.Addresses.Tracker,
			DHT:     s.Addresses.DHT,
			PEX:     s.Addresses.PEX,
		},
		Downloads: struct {
			Total   int
			Running int
			Snubbed int
			Choked  int
		}{
			Total:   s.Downloads.Total,
			Running: s.Downloads.Running,
			Snubbed: s.Downloads.Snubbed,
			Choked:  s.Downloads.Choked,
		},
		MetadataDownloads: struct {
			Total   int
			Snubbed int
			Running int
		}{
			Total:   s.MetadataDownloads.Total,
			Snubbed: s.MetadataDownloads.Snubbed,
			Running: s.MetadataDownloads.Running,
		},
		Name:        s.Name,
		Private:     s.Private,
		FileCount:   s.FileCount,
		PieceLength: s.PieceLength,
		SeededFor:   uint(s.SeededFor / time.Second),
		Speed: struct {
			Download int
			Upload   int
		}{
			Download: s.Speed.Download,
			Upload:   s.Speed.Upload,
		},
	}
	if s.Error != nil {
		reply.Stats.Error = s.Error.Error()
	}
	if s.ETA != nil {
		reply.Stats.ETA = int(*s.ETA / time.Second)
	} else {
		reply.Stats.ETA = -1
	}
	return nil
}

func (h *rpcHandler) GetTorrentTrackers(args *rpctypes.GetTorrentTrackersRequest, reply *rpctypes.GetTorrentTrackersResponse) error {
	t := h.session.GetTorrent(args.ID)
	if t == nil {
		return errTorrentNotFound
	}
	trackers := t.Trackers()
	reply.Trackers = make([]rpctypes.Tracker, len(trackers))
	for i, t := range trackers {
		reply.Trackers[i] = rpctypes.Tracker{
			URL:      t.URL,
			Status:   trackerStatusToString(t.Status),
			Leechers: t.Leechers,
			Seeders:  t.Seeders,
			Warning:  t.Warning,
		}
		if t.Error != nil {
			reply.Trackers[i].Error = t.Error.Error()
			reply.Trackers[i].ErrorInternal = t.Error.err.ErrorWithType()
			reply.Trackers[i].ErrorUnknown = t.Error.Unknown()
		}
		if !t.LastAnnounce.IsZero() {
			reply.Trackers[i].LastAnnounce = rpctypes.Time{Time: t.LastAnnounce}
		}
		if !t.NextAnnounce.IsZero() {
			reply.Trackers[i].NextAnnounce = rpctypes.Time{Time: t.NextAnnounce}
		}
	}
	return nil
}

func (h *rpcHandler) GetTorrentPeers(args *rpctypes.GetTorrentPeersRequest, reply *rpctypes.GetTorrentPeersResponse) error {
	t := h.session.GetTorrent(args.ID)
	if t == nil {
		return errTorrentNotFound
	}
	peers := t.Peers()
	reply.Peers = make([]rpctypes.Peer, len(peers))
	for i, p := range peers {
		var source string
		switch p.Source {
		case SourceTracker:
			source = "TRACKER"
		case SourceDHT:
			source = "DHT"
		case SourcePEX:
			source = "PEX"
		case SourceIncoming:
			source = "INCOMING"
		case SourceManual:
			source = "MANUAL"
		default:
			panic("unhandled peer source")
		}
		reply.Peers[i] = rpctypes.Peer{
			ID:                 hex.EncodeToString(p.ID[:]),
			Client:             p.Client,
			Addr:               p.Addr.String(),
			Source:             source,
			ConnectedAt:        rpctypes.Time{Time: p.ConnectedAt},
			Downloading:        p.Downloading,
			ClientInterested:   p.ClientInterested,
			ClientChoking:      p.ClientChoking,
			PeerInterested:     p.PeerInterested,
			PeerChoking:        p.PeerChoking,
			OptimisticUnchoked: p.OptimisticUnchoked,
			Snubbed:            p.Snubbed,
			EncryptedHandshake: p.EncryptedHandshake,
			EncryptedStream:    p.EncryptedStream,
			DownloadSpeed:      p.DownloadSpeed,
			UploadSpeed:        p.UploadSpeed,
		}
	}
	return nil
}

func (h *rpcHandler) GetTorrentWebseeds(args *rpctypes.GetTorrentWebseedsRequest, reply *rpctypes.GetTorrentWebseedsResponse) error {
	t := h.session.GetTorrent(args.ID)
	if t == nil {
		return errTorrentNotFound
	}
	webseeds := t.Webseeds()
	reply.Webseeds = make([]rpctypes.Webseed, len(webseeds))
	for i, p := range webseeds {
		reply.Webseeds[i] = rpctypes.Webseed{
			URL:           p.URL,
			DownloadSpeed: p.DownloadSpeed,
		}
		if p.Error != nil {
			reply.Webseeds[i].Error = p.Error.Error()
		}
	}
	return nil
}

func (h *rpcHandler) StartTorrent(args *rpctypes.StartTorrentRequest, reply *rpctypes.StartTorrentResponse) error {
	t := h.session.GetTorrent(args.ID)
	if t == nil {
		return errTorrentNotFound
	}
	return t.Start()
}

func (h *rpcHandler) StopTorrent(args *rpctypes.StopTorrentRequest, reply *rpctypes.StopTorrentResponse) error {
	t := h.session.GetTorrent(args.ID)
	if t == nil {
		return errTorrentNotFound
	}
	return t.Stop()
}

func (h *rpcHandler) AnnounceTorrent(args *rpctypes.AnnounceTorrentRequest, reply *rpctypes.AnnounceTorrentResponse) error {
	t := h.session.GetTorrent(args.ID)
	if t == nil {
		return errTorrentNotFound
	}
	t.Announce()
	return nil
}

func (h *rpcHandler) VerifyTorrent(args *rpctypes.VerifyTorrentRequest, reply *rpctypes.VerifyTorrentResponse) error {
	t := h.session.GetTorrent(args.ID)
	if t == nil {
		return errTorrentNotFound
	}
	return t.Verify()
}

func (h *rpcHandler) StartAllTorrents(args *rpctypes.StartAllTorrentsRequest, reply *rpctypes.StartAllTorrentsResponse) error {
	return h.session.StartAll()
}

func (h *rpcHandler) StopAllTorrents(args *rpctypes.StopAllTorrentsRequest, reply *rpctypes.StopAllTorrentsResponse) error {
	return h.session.StopAll()
}

func (h *rpcHandler) AddPeer(args *rpctypes.AddPeerRequest, reply *rpctypes.AddPeerResponse) error {
	t := h.session.GetTorrent(args.ID)
	if t == nil {
		return errTorrentNotFound
	}
	return t.AddPeer(args.Addr)
}

func (h *rpcHandler) AddTracker(args *rpctypes.AddTrackerRequest, reply *rpctypes.AddTrackerResponse) error {
	t := h.session.GetTorrent(args.ID)
	if t == nil {
		return errTorrentNotFound
	}
	return t.AddTracker(args.URL)
}

func (h *rpcHandler) MoveTorrent(args *rpctypes.MoveTorrentRequest, reply *rpctypes.MoveTorrentResponse) error {
	t := h.session.GetTorrent(args.ID)
	if t == nil {
		return errTorrentNotFound
	}
	return t.Move(args.Target)
}

func (h *rpcHandler) handleMoveTorrent(w http.ResponseWriter, r *http.Request) {
	port, err := h.session.getPort()
	if err != nil {
		h.session.log.Error(err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var success bool
	defer func() {
		if !success {
			h.session.releasePort(port)
		}
	}()

	mr, err := r.MultipartReader()
	if err != nil {
		h.session.log.Error(err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// case "id":
	p, err := mr.NextPart()
	if err != nil {
		h.session.log.Error(err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if p.FormName() != "id" {
		http.Error(w, "id expected in multipart form", http.StatusBadRequest)
		return
	}
	b, err := io.ReadAll(p)
	if err != nil {
		h.session.log.Error(err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	id := string(b)
	if id == "" {
		http.Error(w, "id required", http.StatusBadRequest)
		return
	}
	if _, ok := h.session.torrents[id]; ok {
		h.session.log.Warningln("duplicate torrent id, removing existing one:", id)
		t, err := h.session.removeTorrentFromClient(id)
		if err != nil {
			h.session.log.Error(err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		err = h.session.stopAndRemoveData(t)
		if err != nil {
			h.session.log.Error(err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	// case "metadata":
	p, err = mr.NextPart()
	if err != nil {
		h.session.log.Error(err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if p.FormName() != "metadata" {
		http.Error(w, "metadata expected in multipart form", http.StatusBadRequest)
		return
	}
	var s boltdbresumer.Spec
	err = json.NewDecoder(p).Decode(&s)
	if err != nil {
		h.session.log.Error(err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.Port = port
	spec := &s
	// case "data":
	p, err = mr.NextPart()
	if err != nil {
		h.session.log.Error(err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if p.FormName() != "data" {
		http.Error(w, "data expected in multipart form", http.StatusBadRequest)
		return
	}
	err = readData(p, h.session.getDataDir(id), h.session.config.FilePermissions)
	if err != nil {
		h.session.log.Error(err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	err = h.session.resumer.Write(id, spec)
	if err != nil {
		h.session.log.Error(err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	t, started, err := h.session.loadExistingTorrent(id)
	if err != nil {
		h.session.log.Error(err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if started {
		err = t.Start()
		if err != nil {
			h.session.log.Error(err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	success = true
}

func readData(r io.Reader, dir string, perm fs.FileMode) error {
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		name := filepath.Join(dir, hdr.Name)
		err = os.MkdirAll(filepath.Dir(name), os.ModeDir|perm)
		if err != nil {
			return err
		}
		f, err := os.Create(name)
		if err != nil {
			return err
		}
		_, err = io.Copy(f, tr) // nolint: gosec
		if err != nil {
			return err
		}
		err = f.Sync()
		if err != nil {
			return err
		}
		_ = f.Close()
	}
	return nil
}
