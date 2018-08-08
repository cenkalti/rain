package udptracker

type udpAnnounceResponse struct {
	udpMessageHeader
	Interval int32
	Leechers int32
	Seeders  int32
}
