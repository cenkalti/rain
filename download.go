package main

type Download struct {
	TorrentFile *TorrentFile
	Downloaded  int64
	Left        int64
	Uploaded    int64
}
