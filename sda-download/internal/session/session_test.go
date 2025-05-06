package session

import (
	"strings"
	"testing"
	"time"

	"github.com/neicnordic/sda-download/internal/config"
)

func TestNewSessionKey(t *testing.T) {
	// Initialise a cache for testing
	cache, _ := InitialiseSessionCache()
	SessionCache = cache

	// This should generate an UUID4 and verify, that it doesn't already exist in the cache
	// Key verification can't be tested, because it would result in an infinite loop
	key := NewSessionKey()

	// UUID4 is 36 characters long
	expectedLen := 36

	if len(key) != expectedLen {
		t.Errorf("TestNewSessionKey failed, expected key length %d but received %d", expectedLen, len(key))
	}
}

func TestGetSetCache_Found(t *testing.T) {
	// Set expiration time
	config.Config.Session.Expiration = time.Duration(60 * time.Second)

	// Initialise a cache for testing
	cache, _ := InitialiseSessionCache()
	SessionCache = cache

	Set("key1", Cache{Datasets: []string{"dataset1", "dataset2"}})
	time.Sleep(time.Duration(100 * time.Millisecond)) // need to give cache time to get ready
	datasets, exists := Get("key1")

	// Expected results
	expectedDatasets := []string{"dataset1", "dataset2"}
	expectedExists := true

	if strings.Join(datasets.Datasets, "") != strings.Join(expectedDatasets, "") {
		t.Errorf("TestGetSetCache_Found failed, expected %s but received %s", expectedDatasets, datasets)
	}
	if expectedExists != exists {
		t.Errorf("TestGetSetCache_Found failed, expected %t but received %t", expectedExists, exists)
	}
}

func TestGetSetCache_NotFound(t *testing.T) {
	// Set expiration time
	config.Config.Session.Expiration = time.Duration(60 * time.Second)

	// Initialise a cache for testing
	cache, _ := InitialiseSessionCache()
	SessionCache = cache

	Set("key1", Cache{Datasets: []string{"dataset1", "dataset2"}})
	time.Sleep(time.Duration(100 * time.Millisecond)) // need to give cache time to get ready
	datasets, exists := Get("key2")

	// Expected results
	expectedDatasets := []string{}
	expectedExists := false

	if strings.Join(datasets.Datasets, "") != strings.Join(expectedDatasets, "") {
		t.Errorf("TestGetSetCache_NotFound failed, expected %s but received %s", expectedDatasets, datasets)
	}
	if expectedExists != exists {
		t.Errorf("TestGetSetCache_NotFound failed, expected %t but received %t", expectedExists, exists)
	}
}
