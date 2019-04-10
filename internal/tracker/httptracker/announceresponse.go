package httptracker

import (
	"github.com/zeebo/bencode"
)

type announceResponse struct {
	FailureReason  string             `bencode:"failure reason"`
	RetryIn        string             `bencode:"retry in"`
	WarningMessage string             `bencode:"warning message"`
	Interval       int32              `bencode:"interval"`
	MinInterval    int32              `bencode:"min interval"`
	TrackerID      string             `bencode:"tracker id"`
	Complete       int32              `bencode:"complete"`
	Incomplete     int32              `bencode:"incomplete"`
	Peers          bencode.RawMessage `bencode:"peers"`
	ExternalIP     []byte             `bencode:"external ip"`
}
