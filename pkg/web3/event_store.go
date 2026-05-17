package web3

import (
	"fmt"
	"sync"
	"time"
)

// EventStore abstracts persistence for indexed events and checkpoints.
// Production implementations may use PostgreSQL, SQLite, or LevelDB.
//
//go:generate mockgen -destination=mocks/mock_event_store.go -package=mocks streamgate/pkg/web3 EventStore
type EventStore interface {
	// SaveEvent persists an indexed event. Returns error if event already exists.
	SaveEvent(event *IndexedEvent) error

	// SaveCheckpoint persists the last indexed block number for a given contract.
	SaveCheckpoint(contractAddress string, blockNumber uint64) error

	// LoadCheckpoint returns the last indexed block number for a contract.
	// Returns 0 if no checkpoint exists.
	LoadCheckpoint(contractAddress string) (uint64, error)

	// GetEventsByBlockRange returns events in the given block range.
	GetEventsByBlockRange(fromBlock, toBlock uint64) ([]*IndexedEvent, error)

	// MarkEventsReorged marks events from the given block hashes as reorged.
	MarkEventsReorged(blockHashes []string) (int, error)

	// EventExists checks if an event with the given ID already exists.
	EventExists(eventID string) (bool, error)

	// Close releases any resources held by the store.
	Close() error
}

// MemoryEventStore is an in-memory implementation of EventStore for development and testing.
type MemoryEventStore struct {
	mu          sync.RWMutex
	events      map[string]*IndexedEvent   // eventID → event
	checkpoints map[string]uint64          // contractAddress → blockNumber
	blockIndex  map[uint64][]*IndexedEvent // blockNumber → events
}

// NewMemoryEventStore creates a new in-memory event store.
func NewMemoryEventStore() *MemoryEventStore {
	return &MemoryEventStore{
		events:      make(map[string]*IndexedEvent),
		checkpoints: make(map[string]uint64),
		blockIndex:  make(map[uint64][]*IndexedEvent),
	}
}

// SaveEvent persists an indexed event. Returns error if event already exists (dedup).
func (s *MemoryEventStore) SaveEvent(event *IndexedEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.events[event.ID]; exists {
		return fmt.Errorf("event already exists: %s", event.ID)
	}

	s.events[event.ID] = event
	s.blockIndex[event.BlockNumber] = append(s.blockIndex[event.BlockNumber], event)
	return nil
}

// SaveCheckpoint persists the last indexed block number for a given contract.
func (s *MemoryEventStore) SaveCheckpoint(contractAddress string, blockNumber uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.checkpoints[contractAddress] = blockNumber
	return nil
}

// LoadCheckpoint returns the last indexed block number for a contract.
func (s *MemoryEventStore) LoadCheckpoint(contractAddress string) (uint64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.checkpoints[contractAddress], nil
}

// GetEventsByBlockRange returns events in the given block range.
func (s *MemoryEventStore) GetEventsByBlockRange(fromBlock, toBlock uint64) ([]*IndexedEvent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*IndexedEvent
	for block := fromBlock; block <= toBlock; block++ {
		result = append(result, s.blockIndex[block]...)
	}
	return result, nil
}

// MarkEventsReorged marks events from the given block hashes as reorged.
// In the memory store, we add a "reorged" tag to the event's Decoded field.
func (s *MemoryEventStore) MarkEventsReorged(blockHashes []string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	hashSet := make(map[string]bool, len(blockHashes))
	for _, h := range blockHashes {
		hashSet[h] = true
	}

	count := 0
	for _, event := range s.events {
		if hashSet[event.BlockHash] {
			if event.Decoded == nil {
				event.Decoded = make(map[string]interface{})
			}
			event.Decoded["reorged"] = true
			count++
		}
	}
	return count, nil
}

// EventExists checks if an event with the given ID already exists.
func (s *MemoryEventStore) EventExists(eventID string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, exists := s.events[eventID]
	return exists, nil
}

// Close releases any resources. No-op for memory store.
func (s *MemoryEventStore) Close() error {
	return nil
}

// EventCount returns the number of stored events (for testing/monitoring).
func (s *MemoryEventStore) EventCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.events)
}

// GetCheckpointTimestamp returns the time of the last checkpoint save (for monitoring).
func (s *MemoryEventStore) GetCheckpointTimestamp(contractAddress string) time.Time {
	// Memory store doesn't track timestamps, but the interface allows for it
	return time.Time{}
}
