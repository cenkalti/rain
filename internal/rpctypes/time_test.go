package rpctypes

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTimeMarshalJSON(t *testing.T) {
	tm := Time{Time: time.Date(2023, time.January, 2, 15, 4, 5, 0, time.UTC)}
	b, err := json.Marshal(tm)
	require.NoError(t, err)
	assert.Equal(t, `"2023-01-02T15:04:05Z"`, string(b))
}

func TestTimeUnmarshalJSON(t *testing.T) {
	var tm Time
	require.NoError(t, json.Unmarshal([]byte(`"2023-01-02T15:04:05Z"`), &tm))
	assert.True(t, tm.Equal(time.Date(2023, time.January, 2, 15, 4, 5, 0, time.UTC)))
}

func TestTimeRoundTrip(t *testing.T) {
	orig := Time{Time: time.Date(2021, time.June, 30, 23, 59, 59, 0, time.UTC)}
	b, err := json.Marshal(orig)
	require.NoError(t, err)

	var got Time
	require.NoError(t, json.Unmarshal(b, &got))
	assert.True(t, orig.Equal(got.Time))
}

func TestTimeUnmarshalInvalid(t *testing.T) {
	t.Run("not a string", func(t *testing.T) {
		var tm Time
		assert.Error(t, json.Unmarshal([]byte(`12345`), &tm))
	})
	t.Run("not RFC3339", func(t *testing.T) {
		var tm Time
		assert.Error(t, json.Unmarshal([]byte(`"not-a-time"`), &tm))
	})
}
