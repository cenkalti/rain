package httptracker

// scrapeResponse is the response from a scrape request.
type scrapeResponse struct {
	Files         map[string]scrapeFile `bencode:"files"`
	FailureReason string                `bencode:"failure reason"`
	RetryIn       string                `bencode:"retry in"`
}

// scrapeFile contains statistics about a torrent.
type scrapeFile struct {
	Complete   int32 `bencode:"complete"`
	Incomplete int32 `bencode:"incomplete"`
	Downloaded int32 `bencode:"downloaded"`
}