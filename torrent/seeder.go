package torrent

import (
	"context"
	"time"
)

// checkIfOnlySeeder checks if the client is the only seeder for the torrent.
// It returns true if the client is the only seeder, false otherwise.
// If there's an error during the check, it returns false.
func (t *torrent) checkIfOnlySeeder() bool {
	// If we don't have the info yet, we can't check
	if t.info == nil {
		return false
	}

	// If we're not seeding, we're not the only seeder
	if !t.seeding {
		return false
	}

	// Check all trackers
	for _, tr := range t.trackers {
		// Create a context with timeout
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// Scrape the tracker
		resp, err := tr.Scrape(ctx, t.infoHash)
		if err != nil {
			t.log.Debugf("Error scraping tracker %s: %s", tr.URL(), err)
			continue
		}

		// If there are other seeders, we're not the only one
		if resp.Complete > 1 {
			t.log.Debugf("Found %d seeders on tracker %s", resp.Complete, tr.URL())
			return false
		}
	}

	// If we've checked all trackers and we're still here, we're the only seeder
	return true
}

// handleSeedingMode checks if the client should be seeding based on the configuration.
// If SeedOnlyAsLastSeeder is enabled, it will stop seeding if there are other seeders.
func (t *torrent) handleSeedingMode() {
	// If the feature is not enabled, do nothing
	if !t.config.SeedOnlyAsLastSeeder {
		return
	}

	// If we're not seeding, do nothing
	if !t.seeding {
		return
	}

	// Check if we're the only seeder
	isOnlySeeder := t.checkIfOnlySeeder()

	// If we're not the only seeder, stop seeding
	if !isOnlySeeder {
		t.log.Info("Stopping seeding as we're not the only seeder")
		t.seeding = false
	}
}