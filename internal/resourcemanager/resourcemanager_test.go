package resourcemanager

import (
	"testing"
)

func TestResourceManager(t *testing.T) {
	m := New(2)
	if m.Stats().AllocatedObjects != 0 {
		t.FailNow()
	}
	ok := m.Request("foo", nil, 1, nil, nil)
	if !ok {
		t.FailNow()
	}
	if m.Stats().AllocatedObjects != 1 {
		t.FailNow()
	}
	ok = m.Request("foo", nil, 1, nil, nil)
	if !ok {
		t.FailNow()
	}
	if m.Stats().AllocatedObjects != 2 {
		t.FailNow()
	}
	notifyC := make(chan interface{})
	ok = m.Request("foo", "bar", 1, notifyC, nil)
	if ok {
		t.FailNow()
	}
	if m.Stats().AllocatedObjects != 2 {
		t.FailNow()
	}
	m.Release(1)
	if m.Stats().AllocatedObjects != 1 {
		t.FailNow()
	}
	var data interface{}
	select {
	case data = <-notifyC:
	default:
		t.FailNow()
	}
	if data.(string) != "bar" {
		t.FailNow()
	}
	if m.Stats().AllocatedObjects != 2 {
		t.FailNow()
	}
}
