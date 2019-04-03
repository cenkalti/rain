package torrent

func (t *torrent) handleSpeedTicker() {
	t.downloadSpeed.Tick()
	t.uploadSpeed.Tick()
	for _, src := range t.webseedSources {
		src.TickSpeed()
	}
}
