package rainrpc

import (
	"encoding/json"
	"time"
)

type Time struct {
	time.Time
}

var _ json.Marshaler = (*Time)(nil)
var _ json.Unmarshaler = (*Time)(nil)

func (t Time) MarshalJSON() ([]byte, error) {
	return json.Marshal(t.Format(time.RFC3339))
}

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
