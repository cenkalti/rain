package resourcemanager

import (
	"testing"
	"time"
)

func TestResourceManager(t *testing.T) {
	m := New(2)
	ok := m.Request("foo", nil, 1, nil, nil)
	if !ok {
		t.FailNow()
	}
	ok = m.Request("foo", nil, 1, nil, nil)
	if !ok {
		t.FailNow()
	}
	notifyC := make(chan interface{})
	ok = m.Request("foo", "bar", 1, notifyC, nil)
	if ok {
		t.FailNow()
	}
	m.Release(1)
	select {
	case data := <-notifyC:
		if data.(string) != "bar" {
			t.FailNow()
		}
	case <-time.After(time.Second):
		t.FailNow()
	}
}
