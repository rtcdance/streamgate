package web3

import (
	"context"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"go.uber.org/zap"
)

// EventIndexer indexes blockchain events
type EventIndexer struct {
	client          *ethclient.Client
	logger          *zap.Logger
	contractAddress common.Address
	eventSignature  common.Hash
	startBlock      uint64
	currentBlock    uint64
	mu              sync.RWMutex
	events          []*IndexedEvent
	maxEvents       int
	updateInterval  time.Duration
	stopChan        chan struct{}
}

// IndexedEvent represents an indexed blockchain event
type IndexedEvent struct {
	ID              string
	EventType       string
	ContractAddress string
	TransactionHash string
	BlockNumber     uint64
	BlockHash       string
	LogIndex        uint
	Topics          []string
	Data            string
	Timestamp       time.Time
	Decoded         map[string]interface{}
}

// NewEventIndexer creates a new event indexer
func NewEventIndexer(client *ethclient.Client, contractAddress string, eventSignature string, logger *zap.Logger) (*EventIndexer, error) {
	logger.Info("Creating event indexer", "contract", contractAddress, "event_signature", eventSignature)

	return &EventIndexer{
		client:          client,
		logger:          logger,
		contractAddress: common.HexToAddress(contractAddress),
		eventSignature:  common.HexToHash(eventSignature),
		events:          make([]*IndexedEvent, 0, 1000),
		maxEvents:       10000,
		updateInterval:  15 * time.Second,
		stopChan:        make(chan struct{}),
	}, nil
}

// Start starts the event indexer
func (ei *EventIndexer) Start(ctx context.Context) error {
	ei.logger.Info("Starting event indexer")

	// Get current block number
	blockNumber, err := ei.client.BlockNumber(ctx)
	if err != nil {
		ei.logger.Error("Failed to get current block number", zap.Error(err))
		return fmt.Errorf("failed to get current block number: %w", err)
	}

	ei.mu.Lock()
	ei.startBlock = blockNumber
	ei.currentBlock = blockNumber
	ei.mu.Unlock()

	// Start indexing loop
	go ei.indexingLoop(ctx)

	ei.logger.Info("Event indexer started", "start_block", blockNumber)
	return nil
}

// Stop stops the event indexer
func (ei *EventIndexer) Stop() {
	ei.logger.Info("Stopping event indexer")
	close(ei.stopChan)
}

// indexingLoop continuously indexes events
func (ei *EventIndexer) indexingLoop(ctx context.Context) {
	ticker := time.NewTicker(ei.updateInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			ei.indexEvents(ctx)
		case <-ei.stopChan:
			ei.logger.Info("Event indexing loop stopped")
			return
		case <-ctx.Done():
			ei.logger.Info("Event indexing loop cancelled")
			return
		}
	}
}

// indexEvents indexes events from the blockchain
func (ei *EventIndexer) indexEvents(ctx context.Context) {
	ei.mu.RLock()
	currentBlock := ei.currentBlock
	ei.mu.RUnlock()

	// Get latest block
	latestBlock, err := ei.client.BlockNumber(ctx)
	if err != nil {
		ei.logger.Error("Failed to get latest block number", zap.Error(err))
		return
	}

	if latestBlock <= currentBlock {
		return
	}

	// Query events
	query := ethereum.FilterQuery{
		FromBlock: big.NewInt(int64(currentBlock + 1)),
		ToBlock:   big.NewInt(int64(latestBlock)),
		Addresses: []common.Address{ei.contractAddress},
		Topics:    [][]common.Hash{{ei.eventSignature}},
	}

	logs, err := ei.client.FilterLogs(ctx, query)
	if err != nil {
		ei.logger.Error("Failed to filter logs", zap.Error(err))
		return
	}

	ei.logger.Debug("Events indexed", zap.Int("count", len(logs)), "from_block", currentBlock+1, "to_block", latestBlock)

	// Process logs
	for _, log := range logs {
		event := ei.logToEvent(&log)
		ei.addEvent(event)
	}

	// Update current block
	ei.mu.Lock()
	ei.currentBlock = latestBlock
	ei.mu.Unlock()
}

// logToEvent converts a log to an indexed event
func (ei *EventIndexer) logToEvent(log *types.Log) *IndexedEvent {
	topics := make([]string, len(log.Topics))
	for i, topic := range log.Topics {
		topics[i] = topic.Hex()
	}

	event := &IndexedEvent{
		ID:              fmt.Sprintf("%d-%d", log.BlockNumber, log.Index),
		EventType:       "ContractEvent",
		ContractAddress: log.Address.Hex(),
		TransactionHash: log.TxHash.Hex(),
		BlockNumber:     log.BlockNumber,
		BlockHash:       log.BlockHash.Hex(),
		LogIndex:        log.Index,
		Topics:          topics,
		Data:            fmt.Sprintf("0x%x", log.Data),
		Timestamp:       time.Now(),
		Decoded:         make(map[string]interface{}),
	}

	return event
}

// addEvent adds an event to the index
func (ei *EventIndexer) addEvent(event *IndexedEvent) {
	ei.mu.Lock()
	defer ei.mu.Unlock()

	ei.events = append(ei.events, event)

	// Keep only recent events
	if len(ei.events) > ei.maxEvents {
		ei.events = ei.events[len(ei.events)-ei.maxEvents:]
	}
}

// GetEvents returns all indexed events
func (ei *EventIndexer) GetEvents() []*IndexedEvent {
	ei.mu.RLock()
	defer ei.mu.RUnlock()

	result := make([]*IndexedEvent, len(ei.events))
	copy(result, ei.events)
	return result
}

// GetEventsByType returns events of a specific type
func (ei *EventIndexer) GetEventsByType(eventType string) []*IndexedEvent {
	ei.mu.RLock()
	defer ei.mu.RUnlock()

	result := make([]*IndexedEvent, 0)
	for _, event := range ei.events {
		if event.EventType == eventType {
			result = append(result, event)
		}
	}

	return result
}

// GetEventsByBlockRange returns events in a block range
func (ei *EventIndexer) GetEventsByBlockRange(fromBlock uint64, toBlock uint64) []*IndexedEvent {
	ei.mu.RLock()
	defer ei.mu.RUnlock()

	result := make([]*IndexedEvent, 0)
	for _, event := range ei.events {
		if event.BlockNumber >= fromBlock && event.BlockNumber <= toBlock {
			result = append(result, event)
		}
	}

	return result
}

// GetEventCount returns the number of indexed events
func (ei *EventIndexer) GetEventCount() int {
	ei.mu.RLock()
	defer ei.mu.RUnlock()

	return len(ei.events)
}

// GetCurrentBlock returns the current indexed block
func (ei *EventIndexer) GetCurrentBlock() uint64 {
	ei.mu.RLock()
	defer ei.mu.RUnlock()

	return ei.currentBlock
}

// EventListener listens for specific events
type EventListener struct {
	indexer  *EventIndexer
	logger   *zap.Logger
	handlers map[string][]EventHandler
	mu       sync.RWMutex
}

// EventHandler is a function that handles events
type EventHandler func(ctx context.Context, event *IndexedEvent) error

// NewEventListener creates a new event listener
func NewEventListener(indexer *EventIndexer, logger *zap.Logger) *EventListener {
	return &EventListener{
		indexer:  indexer,
		logger:   logger,
		handlers: make(map[string][]EventHandler),
	}
}

// On registers a handler for an event type
func (el *EventListener) On(eventType string, handler EventHandler) {
	el.mu.Lock()
	defer el.mu.Unlock()

	el.handlers[eventType] = append(el.handlers[eventType], handler)
	el.logger.Debug("Event handler registered", zap.String("event_type", eventType))
}

// Emit emits an event to all registered handlers
func (el *EventListener) Emit(ctx context.Context, event *IndexedEvent) error {
	el.mu.RLock()
	handlers, exists := el.handlers[event.EventType]
	el.mu.RUnlock()

	if !exists {
		return nil
	}

	for _, handler := range handlers {
		if err := handler(ctx, event); err != nil {
			el.logger.Error("Error handling event", "event_type", event.EventType, "error", err)
		}
	}

	return nil
}

// ProcessAllEvents processes all indexed events
func (el *EventListener) ProcessAllEvents(ctx context.Context) error {
	el.logger.Info("Processing all indexed events")

	events := el.indexer.GetEvents()
	for _, event := range events {
		if err := el.Emit(ctx, event); err != nil {
			el.logger.Error("Error processing event", "event_id", event.ID, "error", err)
		}
	}

	el.logger.Info("All events processed", zap.Int("count", len(events)))
	return nil
}

// ContentRegisteredEvent represents a content registered event
type ContentRegisteredEvent struct {
	ContentHash string
	Owner       string
	Timestamp   int64
	Metadata    string
}

// DecodeContentRegisteredEvent decodes a content registered event
func DecodeContentRegisteredEvent(event *IndexedEvent) (*ContentRegisteredEvent, error) {
	// TODO: Implement event decoding
	return &ContentRegisteredEvent{
		ContentHash: event.Topics[1],
		Owner:       event.Topics[2],
	}, nil
}

// NFTMintedEvent represents an NFT minted event
type NFTMintedEvent struct {
	TokenID string
	Owner   string
	URI     string
}

// DecodeNFTMintedEvent decodes an NFT minted event
func DecodeNFTMintedEvent(event *IndexedEvent) (*NFTMintedEvent, error) {
	// TODO: Implement event decoding
	return &NFTMintedEvent{
		TokenID: event.Topics[3],
		Owner:   event.Topics[2],
	}, nil
}
