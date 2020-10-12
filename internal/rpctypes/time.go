package rpctypes

import (
	"encoding/json"
	"time"
)

// Time is a wrapper around time.Time. Serialized as RFC3339 string.
type Time struct {
	time.Time
}

var (
	_ json.Marshaler   = (*Time)(nil)
	_ json.Unmarshaler = (*Time)(nil)
)

// MarshalJSON converts the time into RFC3339 string.
func (t Time) MarshalJSON() ([]byte, error) {
	return json.Marshal(t.Format(time.RFC3339))
}

// UnmarshalJSON sets the time from a RFC3339 string.
func (t *Time) UnmarshalJSON(b []byte) error {
	var s string
	err := json.Unmarshal(b, &s)
	if err != nil {
		return err
	}
	t2, err := time.Parse(time.RFC3339, s)
	t.Time = t2
	return err
}
