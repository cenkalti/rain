package rain

import "sync"

// Manager returns the same instance for same URLs.
type trackerManager struct {
	trackers map[string]*trackerAndCount
	client   *Client
	m        sync.Mutex
}

type trackerAndCount struct {
	tracker *managedTracker
	count   uint
}

func newManager(c *Client) *trackerManager {
	return &trackerManager{
		trackers: make(map[string]*trackerAndCount),
		client:   c,
	}
}

func (m *trackerManager) NewTracker(trackerURL string) (*managedTracker, error) {
	m.m.Lock()
	defer m.m.Unlock()
	entry, ok := m.trackers[trackerURL]
	if ok {
		entry.count++
		return entry.tracker, nil
	}
	t, err := newTracker(trackerURL, m.client)
	if err != nil {
		return nil, err
	}
	mt := &managedTracker{manager: m, tracker: t}
	m.trackers[trackerURL] = &trackerAndCount{tracker: mt, count: 1}
	return mt, nil
}

type managedTracker struct {
	manager *trackerManager
	tracker
}

func (t *managedTracker) Close() error {
	t.manager.m.Lock()
	entry := t.manager.trackers[t.URL()]
	entry.count--
	if entry.count == 0 {
		delete(t.manager.trackers, t.URL())
		t.manager.m.Unlock()
		return entry.tracker.tracker.Close()
	}
	t.manager.m.Unlock()
	return nil
}
