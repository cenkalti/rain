package rain

import (
	"os"
	"testing"
)

func TestConfigLoadSave(t *testing.T) {
	// Setup
	const filename = "/tmp/rain-test-config.yaml"
	err := os.Remove(filename)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}

	// Test new config
	c, err := LoadConfig(filename)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%+v", c)
	c.Port = 6
	err = c.Save()
	if err != nil {
		t.Fatal(err)
	}

	// Test existing config
	c, err = LoadConfig(filename)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%+v", c)
	if c.Port != 6 {
		t.Fatal("invalid port in config")
	}

	// Cleanup
	err = os.Remove(filename)
	if err != nil {
		t.Fatal(err)
	}
}
