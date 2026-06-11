package torrent

import (
	"archive/tar"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/cenkalti/rain/v2/internal/resumer/boltdbresumer"
)

func (h *rpcHandler) handleMoveTorrent(w http.ResponseWriter, r *http.Request) {
	var provider *fileStorageProvider
	var ok bool
	if provider, ok = h.session.storage.(*fileStorageProvider); !ok {
		err := errors.New("session is not using file storage")
		h.session.log.Error(err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

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
		err = h.session.stopAndRemoveData(t, false)
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
	err = readData(p, provider.getDataDir(id), h.session.config.FilePermissions)
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
	dir = filepath.Clean(dir)
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
		// Reject entries that escape the destination directory (zip/tar slip).
		// filepath.Join cleans ".." segments, so a malicious header name
		// would otherwise resolve to a path outside dir. Legitimate archives
		// produced by generateTar only contain regular files nested under dir,
		// so every valid entry has the dir+separator prefix.
		if !strings.HasPrefix(name, dir+string(os.PathSeparator)) {
			return fmt.Errorf("tar entry %q escapes destination directory", hdr.Name)
		}
		err = os.MkdirAll(filepath.Dir(name), os.ModeDir|perm)
		if err != nil {
			return err
		}
		err = writeFile(name, tr)
		if err != nil {
			return err
		}
	}
	return nil
}

// writeFile creates name and copies r into it, syncing before close. The file
// handle is closed on every path so a failed entry does not leak a descriptor.
func writeFile(name string, r io.Reader) error {
	f, err := os.Create(name)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, r) // nolint: gosec
	if err != nil {
		return err
	}
	return f.Sync()
}
