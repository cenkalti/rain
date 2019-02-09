package resourcemanager

import (
	"testing"
	"time"
)

func TestResourceManager(t *testing.T) {
	m := New(2)
	ok := m.Request("foo", 1, nil, nil)
	if !ok {
		t.FailNow()
	}
	ok = m.Request("foo", 1, nil, nil)
	if !ok {
		t.FailNow()
	}
	notifyC := make(chan struct{})
	ok = m.Request("foo", 1, notifyC, nil)
	if ok {
		t.FailNow()
	}
	m.Release(1)
	select {
	case <-notifyC:
	case <-time.After(time.Second):
		t.FailNow()
	}
}
