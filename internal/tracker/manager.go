package tracker

import "sync"

// Manager returns the same instance for same URLs.
type Manager struct {
	trackers map[string]*trackerAndCount
	client   Client
	m        sync.Mutex
}

type trackerAndCount struct {
	tracker *managedTracker
	count   uint
}

func NewManager(c Client) *Manager {
	return &Manager{
		trackers: make(map[string]*trackerAndCount),
		client:   c,
	}
}

func (m *Manager) NewTracker(trackerURL string) (*managedTracker, error) {
	m.m.Lock()
	defer m.m.Unlock()
	entry, ok := m.trackers[trackerURL]
	if ok {
		entry.count++
		return entry.tracker, nil
	}
	t, err := New(trackerURL, m.client)
	if err != nil {
		return nil, err
	}
	mt := &managedTracker{tracker: t}
	m.trackers[trackerURL] = &trackerAndCount{tracker: mt, count: 1}
	return mt, nil
}

type managedTracker struct {
	manager *Manager
	tracker Tracker
}

func (t *managedTracker) URL() string {
	return t.URL()
}

func (t *managedTracker) Announce(transfer Transfer, e Event) (*AnnounceResponse, error) {
	return t.Announce(transfer, e)
}

func (t *managedTracker) Close() error {
	t.manager.m.Lock()
	entry := t.manager.trackers[t.URL()]
	entry.count--
	if entry.count == 0 {
		delete(t.manager.trackers, t.URL())
		t.manager.m.Unlock()
		return entry.tracker.Close()
	}
	t.manager.m.Unlock()
	return nil
}
