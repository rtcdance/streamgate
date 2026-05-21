package event

import (
	"fmt"
	"sync"
	"time"
)

//go:generate mockgen -destination=../mocks/mock_event_store.go -package=mocks streamgate/pkg/web3/event EventStore

type EventStore interface {
	SaveEvent(event *IndexedEvent) error
	SaveCheckpoint(contractAddress string, blockNumber uint64) error
	LoadCheckpoint(contractAddress string) (uint64, error)
	GetEventsByBlockRange(fromBlock, toBlock uint64) ([]*IndexedEvent, error)
	MarkEventsReorged(blockHashes []string) (int, error)
	EventExists(eventID string) (bool, error)
	Close() error
}

type MemoryEventStore struct {
	mu          sync.RWMutex
	events      map[string]*IndexedEvent
	checkpoints map[string]uint64
	blockIndex  map[uint64][]*IndexedEvent
}

func NewMemoryEventStore() *MemoryEventStore {
	return &MemoryEventStore{
		events:      make(map[string]*IndexedEvent),
		checkpoints: make(map[string]uint64),
		blockIndex:  make(map[uint64][]*IndexedEvent),
	}
}

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

func (s *MemoryEventStore) SaveCheckpoint(contractAddress string, blockNumber uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.checkpoints[contractAddress] = blockNumber
	return nil
}

func (s *MemoryEventStore) LoadCheckpoint(contractAddress string) (uint64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.checkpoints[contractAddress], nil
}

func (s *MemoryEventStore) GetEventsByBlockRange(fromBlock, toBlock uint64) ([]*IndexedEvent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*IndexedEvent
	for block := fromBlock; block <= toBlock; block++ {
		result = append(result, s.blockIndex[block]...)
	}
	return result, nil
}

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

func (s *MemoryEventStore) EventExists(eventID string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, exists := s.events[eventID]
	return exists, nil
}

func (s *MemoryEventStore) Close() error {
	return nil
}

func (s *MemoryEventStore) EventCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.events)
}

func (s *MemoryEventStore) GetCheckpointTimestamp(contractAddress string) time.Time {
	return time.Time{}
}
