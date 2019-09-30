package torrent

import (
	"archive/tar"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/boltdb/bolt"
	"github.com/cenkalti/rain/internal/resumer/boltdbresumer"
	"github.com/cenkalti/rain/internal/tracker"
)

// Torrent is created from a torrent file or a magnet link.
type Torrent struct {
	torrent *torrent
}

// InfoHash is the unique value that represents the files in a torrent.
type InfoHash [20]byte

// String encodes info hash in hex as 40 characters.
func (h InfoHash) String() string {
	return hex.EncodeToString(h[:])
}

// ID is a unique identifier in the Session.
func (t *Torrent) ID() string {
	return t.torrent.id
}

// Name of the torrent.
// For magnet downloads name can change after metadata is downloaded but this method still returns the initial name.
// Use Stats() method to get name in info dictionary.
func (t *Torrent) Name() string {
	return t.torrent.Name()
}

// InfoHash returns the hash of the info dictionary of torrent file.
// Two different torrents may have the same info hash.
func (t *Torrent) InfoHash() InfoHash {
	var ih InfoHash
	copy(ih[:], t.torrent.InfoHash())
	return ih
}

// AddedAt returns the time that the torrent is added.
func (t *Torrent) AddedAt() time.Time {
	return t.torrent.addedAt
}

// Stats returns statistics about the torrent.
func (t *Torrent) Stats() Stats {
	return t.torrent.Stats()
}

// Magnet returns the magnet link.
// Returns error if torrent is private.
func (t *Torrent) Magnet() (string, error) {
	return t.torrent.Magnet()
}

// Torrent returns the metainfo bytes (contents of .torrent file).
// Returns error if torrent has no metadata yet.
func (t *Torrent) Torrent() ([]byte, error) {
	return t.torrent.Torrent()
}

// Trackers returns the list of trackers of this torrent.
func (t *Torrent) Trackers() []Tracker {
	return t.torrent.Trackers()
}

// Peers returns the list of connected (handshake completed) peers of the torrent.
func (t *Torrent) Peers() []Peer {
	return t.torrent.Peers()
}

// Webseeds returns the list of WebSeed sources in the torrent.
func (t *Torrent) Webseeds() []Webseed {
	return t.torrent.Webseeds()
}

// Port returns the TCP port number that the torrent is listening peers.
func (t *Torrent) Port() int {
	return t.torrent.port
}

// NotifyStop returns a new channel for notifying stop event.
// Value from the channel contains the error if there is any, otherwise the value is nil.
// NotifyStop must be called after calling Start().
func (t *Torrent) NotifyStop() <-chan error {
	return t.torrent.NotifyError()
}

// NotifyComplete returns a channel for notifying completion.
// The channel is closed once all pieces are downloaded successfully.
// NotifyComplete must be called after calling Start().
func (t *Torrent) NotifyComplete() <-chan struct{} {
	return t.torrent.NotifyComplete()
}

// AddPeer adds a new peer to the torrent. Does nothing if torrent is stopped.
func (t *Torrent) AddPeer(addr string) error {
	return t.torrent.addPeerString(addr)
}

// AddTracker adds a new tracker to the torrent.
func (t *Torrent) AddTracker(uri string) error {
	var private bool
	if t.torrent.info != nil {
		private = t.torrent.info.Private
	}
	tr, err := t.torrent.session.trackerManager.Get(uri, t.torrent.session.config.TrackerHTTPTimeout, t.torrent.session.getTrackerUserAgent(private), int64(t.torrent.session.config.TrackerHTTPMaxResponseSize))
	if err != nil {
		return err
	}
	err = t.torrent.session.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(torrentsBucket).Bucket([]byte(t.torrent.id))
		value := b.Get(boltdbresumer.Keys.Trackers)
		var trackers [][]string
		err = json.Unmarshal(value, &trackers)
		if err != nil {
			return err
		}
		trackers = append(trackers, []string{uri})
		value, err = json.Marshal(trackers)
		if err != nil {
			return err
		}
		return b.Put(boltdbresumer.Keys.Trackers, value)
	})
	if err != nil {
		return err
	}
	t.torrent.AddTrackers([]tracker.Tracker{tr})
	return nil
}

// Start downloading the torrent. If all pieces are completed, starts seeding them.
func (t *Torrent) Start() error {
	err := t.torrent.session.resumer.WriteStarted(t.torrent.id, true)
	if err != nil {
		return err
	}
	t.torrent.Start()
	return nil
}

// Stop the torrent. Does not block. After Stop is called, the torrent switches into Stopping state.
// During Stopping state, a stop event sent to trackers with a timeout.
// At most 5 seconds later, the torrent switches into Stopped state.
func (t *Torrent) Stop() error {
	err := t.torrent.session.resumer.WriteStarted(t.torrent.id, false)
	if err != nil {
		return err
	}
	t.torrent.Stop()
	return nil
}

// Announce the torrent to all trackers and DHT. It does not overrides the minimum interval value sent by the trackers or set in Config.
func (t *Torrent) Announce() {
	t.torrent.Announce()
}

// Verify pieces of torrent by reading all of the torrents files from disk.
// After Verify called, the torrent is stopped, then verification starts and the torrent switches into Verifying state.
// The torrent stays stopped after verification finishes.
func (t *Torrent) Verify() error {
	err := t.torrent.session.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(torrentsBucket).Bucket([]byte(t.torrent.id))
		return b.Delete([]byte("bitfield"))
	})
	if err != nil {
		return err
	}
	t.torrent.Verify()
	return nil
}

// Move torrent to another Session.
// target must be the RPC server address in host:port form.
func (t *Torrent) Move(target string) error {
	t.torrent.Stop()
	spec, err := t.torrent.session.resumer.Read(t.torrent.id)
	if err != nil {
		return err
	}

	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	go t.prepareBody(pw, mw, spec)

	req, err := http.NewRequest(http.MethodPost, target+"/move-torrent?id="+t.torrent.id, pr)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("http error: %d", resp.StatusCode)
	}
	defer resp.Body.Close()
	return t.torrent.session.RemoveTorrent(t.torrent.id)
}

func (t *Torrent) prepareBody(pw *io.PipeWriter, mw *multipart.Writer, spec *boltdbresumer.Spec) {
	var err error
	defer func() { _ = pw.CloseWithError(err) }()

	iw, err := mw.CreateFormField("id")
	if err != nil {
		t.torrent.log.Errorln("cannot create id form filed:", err)
		return
	}
	_, err = iw.Write([]byte(t.torrent.id))
	if err != nil {
		t.torrent.log.Errorln("cannot write id:", err)
		return
	}
	fw, err := mw.CreateFormField("metadata")
	if err != nil {
		t.torrent.log.Errorln("cannot create metadata form filed:", err)
		return
	}
	err = json.NewEncoder(fw).Encode(spec)
	if err != nil {
		t.torrent.log.Errorln("cannot encode resumer spec:", err)
		return
	}
	dw, err := mw.CreateFormField("data")
	if err != nil {
		t.torrent.log.Errorln("cannot create data form filed:", err)
		return
	}
	tpr, tpw := io.Pipe()
	go t.generateTar(tpw)
	_, err = io.Copy(dw, tpr)
	if err != nil {
		t.torrent.log.Errorln("error copying pipe:", err)
		return
	}
	err = mw.Close()
	if err != nil {
		t.torrent.log.Errorln("cannot close multipart writer:", err)
		return
	}
}

func (t *Torrent) generateTar(pw *io.PipeWriter) {
	var err error
	defer func() { _ = pw.CloseWithError(err) }()

	tw := tar.NewWriter(pw)
	var root string
	if t.torrent.session.config.DataDirIncludesTorrentID {
		root = filepath.Join(t.torrent.session.config.DataDir, t.torrent.id)
	} else {
		root = t.torrent.session.config.DataDir
	}
	walkFunc := func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		hdr := &tar.Header{
			Name: path[len(root)+1:],
			Mode: 0600,
			Size: info.Size(),
		}
		err = tw.WriteHeader(hdr)
		if err != nil {
			t.torrent.log.Errorln("cannot write tar header:", err)
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			t.torrent.log.Errorln("cannot open file:", err)
			return err
		}
		_, err = io.Copy(tw, f)
		f.Close()
		if err != nil {
			t.torrent.log.Errorln("cannot copy storage file to tar writer:", err)
			return err
		}
		return nil
	}
	err = filepath.Walk(root, walkFunc)
	if os.IsNotExist(err) {
		err = nil
		return
	}
	if err != nil {
		t.torrent.log.Errorln("error walking files:", err)
		return
	}
	err = tw.Close()
	if err != nil {
		t.torrent.log.Errorln("cannot close tar writer:", err)
		return
	}
}
