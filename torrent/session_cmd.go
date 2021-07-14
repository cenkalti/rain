package torrent

import (
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
)

func (s *Session) runOnCompleteCmd(torrent *torrent) {
	command, err := exec.LookPath(s.config.OnCompleteCmd[0])
	if err != nil {
		s.log.Errorf("error resolving completion hook command path: %s", err)
		return
	}

	cmd := exec.Command(command)
	if len(s.config.OnCompleteCmd) > 1 {
		cmd.Args = append(cmd.Args, s.config.OnCompleteCmd[1:]...)
	}

	cmd.Env = append(os.Environ(),
		"RAIN_TORRENT_ADDED="+fmt.Sprint(torrent.addedAt.Unix()),
		"RAIN_TORRENT_DIR="+torrent.storage.RootDir(),
		"RAIN_TORRENT_HASH="+hex.EncodeToString(torrent.infoHash[:]),
		"RAIN_TORRENT_ID="+torrent.id,
		"RAIN_TORRENT_NAME="+torrent.name)

	s.log.Debugf("executing completion hook for torrent %s: %s", torrent.id, cmd.String())

	if err := cmd.Run(); err != nil {
		s.log.Errorf("completion hook execution failed: %s", err)
	}
}
