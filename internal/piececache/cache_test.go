package piececache

import (
	"errors"
	"testing"
	"time"
)

func TestCache(t *testing.T) {
	c := New(10, time.Minute, 1)

	// Test empty cache
	if len(c.items) != 0 {
		t.FailNow()
	}
	if len(c.accessList) != 0 {
		t.FailNow()
	}
	if c.size != 0 {
		t.FailNow()
	}

	// Test load item
	var loaded bool
	fooLoader := func() ([]byte, error) {
		loaded = true
		return []byte("bar"), nil
	}
	val, err := c.Get("foo", fooLoader)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded {
		t.FailNow()
	}
	if string(val) != "bar" {
		t.FailNow()
	}
	if len(c.items) != 1 {
		t.FailNow()
	}
	if len(c.accessList) != 1 {
		t.FailNow()
	}
	if c.size != 3 {
		t.FailNow()
	}

	// Test cached get
	loaded = false
	val, err = c.Get("foo", fooLoader)
	if err != nil {
		t.Fatal(err)
	}
	if loaded {
		t.FailNow()
	}
	if string(val) != "bar" {
		t.FailNow()
	}
	if len(c.items) != 1 {
		t.FailNow()
	}
	if len(c.accessList) != 1 {
		t.FailNow()
	}
	if c.size != 3 {
		t.FailNow()
	}

	// Test load error
	errLoad := errors.New("load error")
	errLoader := func() ([]byte, error) {
		return nil, errLoad
	}
	_, err = c.Get("foo2", errLoader)
	if err != errLoad {
		t.Fatal(err)
	}
	if len(c.items) != 1 {
		t.FailNow()
	}
	if len(c.accessList) != 1 {
		t.FailNow()
	}
	if c.size != 3 {
		t.FailNow()
	}

	// Test delete oldest
	loader8 := func() ([]byte, error) {
		return []byte("12345678"), nil
	}
	val, err = c.Get("foo8", loader8)
	if err != nil {
		t.Fatal(err)
	}
	if string(val) != "12345678" {
		t.FailNow()
	}
	if len(c.items) != 1 {
		t.FailNow()
	}
	if len(c.accessList) != 1 {
		t.FailNow()
	}
	if c.size != 8 {
		t.FailNow()
	}

	// Test oversized item
	oversized := func() ([]byte, error) {
		return []byte("12345678901"), nil
	}
	val, err = c.Get("oversized", oversized)
	if err != nil {
		t.Fatal(err)
	}
	if string(val) != "12345678901" {
		t.FailNow()
	}
	if len(c.items) != 1 {
		t.FailNow()
	}
	if len(c.accessList) != 1 {
		t.FailNow()
	}
	if c.size != 8 {
		t.FailNow()
	}

	// Test second item
	second := func() ([]byte, error) {
		return []byte("a"), nil
	}
	val, err = c.Get("second", second)
	if err != nil {
		t.Fatal(err)
	}
	if string(val) != "a" {
		t.FailNow()
	}
	if len(c.items) != 2 {
		t.FailNow()
	}
	if len(c.accessList) != 2 {
		t.FailNow()
	}
	if c.size != 9 {
		t.FailNow()
	}
	if c.accessList[0].key != "foo8" {
		t.FailNow()
	}
	if c.accessList[1].key != "second" {
		t.FailNow()
	}

	// Test update access time
	val, err = c.Get("foo8", nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(val) != "12345678" {
		t.FailNow()
	}
	if len(c.items) != 2 {
		t.FailNow()
	}
	if len(c.accessList) != 2 {
		t.FailNow()
	}
	if c.size != 9 {
		t.FailNow()
	}
	if c.accessList[0].key != "second" {
		t.FailNow()
	}
	if c.accessList[1].key != "foo8" {
		t.FailNow()
	}
}

func TestTTL(t *testing.T) {
	const ttl = 100 * time.Millisecond

	c := New(10, ttl, 1)

	var loaded bool
	fooLoader := func() ([]byte, error) {
		loaded = true
		return []byte("bar"), nil
	}
	val, err := c.Get("foo", fooLoader)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded {
		t.FailNow()
	}
	if string(val) != "bar" {
		t.FailNow()
	}

	// Must not load
	loaded = false
	val, err = c.Get("foo", fooLoader)
	if err != nil {
		t.Fatal(err)
	}
	if loaded {
		t.FailNow()
	}
	if string(val) != "bar" {
		t.FailNow()
	}

	// Must load again
	time.Sleep(ttl + 10*time.Millisecond)

	loaded = false
	val, err = c.Get("foo", fooLoader)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded {
		t.FailNow()
	}
	if string(val) != "bar" {
		t.FailNow()
	}
}

func TestClear(t *testing.T) {
	const ttl = 100 * time.Millisecond

	c := New(10, ttl, 1)

	var loaded bool
	fooLoader := func() ([]byte, error) {
		loaded = true
		return []byte("bar"), nil
	}
	val, err := c.Get("foo", fooLoader)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded {
		t.FailNow()
	}
	if string(val) != "bar" {
		t.FailNow()
	}

	c.Clear()

	time.Sleep(ttl + 10*time.Millisecond)
}
