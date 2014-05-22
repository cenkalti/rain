package main

// Download represents an active download in the program.
type Download struct {
	TorrentFile *TorrentFile
	Downloaded  int64
	Left        int64
	Uploaded    int64
}
