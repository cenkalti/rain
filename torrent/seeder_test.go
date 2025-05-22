package torrent

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cenkalti/rain/v2/internal/logger"
	"github.com/cenkalti/rain/v2/internal/metainfo"
	"github.com/cenkalti/rain/v2/internal/tracker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockTracker is a mock implementation of the Tracker interface
type MockTracker struct {
	mock.Mock
}

func (m *MockTracker) URL() string {
	args := m.Called()
	return args.String(0)
}

func (m *MockTracker) Announce(ctx context.Context, transfer tracker.Transfer, event tracker.Event, numWant int) (*tracker.AnnounceResponse, error) {
	args := m.Called(ctx, transfer, event, numWant)
	return args.Get(0).(*tracker.AnnounceResponse), args.Error(1)
}

func (m *MockTracker) Scrape(ctx context.Context, ih [20]byte) (*tracker.ScrapeResponse, error) {
	args := m.Called(ctx, ih)
	return args.Get(0).(*tracker.ScrapeResponse), args.Error(1)
}

func (m *MockTracker) Close() error {
	args := m.Called()
	return args.Error(0)
}

func TestCheckIfOnlySeeder(t *testing.T) {
	// Create a mock torrent
	tor := &torrent{
		seeding: true,
		info:    &metainfo.Info{},
	}

	// Create a mock tracker
	mockTracker := new(MockTracker)
	mockTracker.On("URL").Return("http://example.com/announce")
	
	// Test case 1: We are the only seeder
	mockTracker.On("Scrape", mock.Anything, mock.Anything).Return(&tracker.ScrapeResponse{
		Complete:   1, // Only one seeder (us)
		Incomplete: 5,
		Downloaded: 10,
	}, nil).Once()
	
	tor.trackers = []tracker.Tracker{mockTracker}
	
	// We should be the only seeder
	assert.True(t, tor.checkIfOnlySeeder())
	
	// Test case 2: There are other seeders
	mockTracker.On("Scrape", mock.Anything, mock.Anything).Return(&tracker.ScrapeResponse{
		Complete:   3, // Multiple seeders
		Incomplete: 5,
		Downloaded: 10,
	}, nil).Once()
	
	// We should not be the only seeder
	assert.False(t, tor.checkIfOnlySeeder())
	
	// Test case 3: Error during scrape
	mockTracker.On("Scrape", mock.Anything, mock.Anything).Return(&tracker.ScrapeResponse{}, 
		errors.New("scrape error")).Once()
	
	// If there's an error, we should assume we're not the only seeder
	assert.False(t, tor.checkIfOnlySeeder())
	
	// Test case 4: Not seeding
	tor.seeding = false
	
	// If we're not seeding, we're not the only seeder
	assert.False(t, tor.checkIfOnlySeeder())
	
	mockTracker.AssertExpectations(t)
}

func TestHandleSeedingMode(t *testing.T) {
	// Create a mock torrent
	tor := &torrent{
		seeding: true,
		info:    &metainfo.Info{},
		config:  Config{SeedOnlyAsLastSeeder: true},
		log:     logger.New("torrent"),
	}
	
	// Create a mock tracker
	mockTracker := new(MockTracker)
	mockTracker.On("URL").Return("http://example.com/announce")
	
	// Test case 1: We are the only seeder
	mockTracker.On("Scrape", mock.Anything, mock.Anything).Return(&tracker.ScrapeResponse{
		Complete:   1, // Only one seeder (us)
		Incomplete: 5,
		Downloaded: 10,
	}, nil).Once()
	
	tor.trackers = []tracker.Tracker{mockTracker}
	
	// Call the function
	tor.handleSeedingMode()
	
	// We should still be seeding
	assert.True(t, tor.seeding)
	
	// Test case 2: There are other seeders
	mockTracker.On("Scrape", mock.Anything, mock.Anything).Return(&tracker.ScrapeResponse{
		Complete:   3, // Multiple seeders
		Incomplete: 5,
		Downloaded: 10,
	}, nil).Once()
	
	// Call the function
	tor.handleSeedingMode()
	
	// We should stop seeding
	assert.False(t, tor.seeding)
	
	// Test case 3: Feature disabled
	tor.seeding = true
	tor.config.SeedOnlyAsLastSeeder = false
	
	// Call the function
	tor.handleSeedingMode()
	
	// We should still be seeding
	assert.True(t, tor.seeding)
	
	mockTracker.AssertExpectations(t)
}