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
	"go.uber.org/zap"
)

// EventReader abstracts the event indexing methods needed by EventIndexer.
// Obtain via ChainClient.GetEthClient() or provide a mock for testing.
//go:generate mockgen -destination=mocks/mock_event_reader.go -package=mocks streamgate/pkg/web3 EventReader
type EventReader interface {
	BlockNumber(ctx context.Context) (uint64, error)
	FilterLogs(ctx context.Context, q ethereum.FilterQuery) ([]types.Log, error)
}

// EventIndexerConfig holds configuration for event indexing with filtering.
type EventIndexerConfig struct {
	ContractAddresses  []string        // filter to these (empty = all)
	EventSignatures    []string        // filter to these topic0 hashes (empty = all)
	StartBlock         uint64          // block to start indexing from (0 = latest)
	MaxEvents          int             // max events to buffer (default 10000)
	UpdateInterval     time.Duration   // polling interval (default 15s)
	ConfirmationBlocks uint64          // safety buffer: only index up to latestBlock - N (default 12, 0 = index to tip)
}

// EventIndexer indexes blockchain events using WebSocket subscriptions
// when available, falling back to polling when WS is unavailable.
type EventIndexer struct {
	client             EventReader
	logger             *zap.Logger
	contractAddress    common.Address       // primary contract (legacy single-address)
	eventSignature     common.Hash          // primary event sig (legacy single-sig)
	contractAddresses []common.Address      // filtered contract addresses
	eventSignatures   []common.Hash         // filtered event signatures
	startBlock        uint64
	currentBlock      uint64
	mu                sync.RWMutex
	events            []*IndexedEvent
	maxEvents         int
	updateInterval    time.Duration
	stopChan          chan struct{}
	modeCh            chan string    // signals indexingLoop to switch mode ("polling")
	subscriber        *LogSubscriber // WebSocket subscriber (nil = polling only)
	mode              string         // "websocket" or "polling"
	store             EventStore     // optional: persistent event storage + checkpoint
	reorgDetector     *ReorgDetector // optional: reorg detection for indexed events
	stopOnce          sync.Once      // ensures stopChan is closed only once
	wg                sync.WaitGroup // tracks background goroutines
	eventParser       *EventParser   // optional: decode event data into Decoded field
	seenIDs           map[string]struct{} // in-memory dedup when EventStore is nil
	confirmationBlocks uint64         // safety buffer: only index up to latestBlock - N
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

// NewEventIndexer creates a new event indexer.
// wsURL is optional — if empty or a WS connection fails, it falls back to polling.
func NewEventIndexer(client EventReader, contractAddress, eventSignature string, logger *zap.Logger, wsURL ...string) (*EventIndexer, error) {
	cfg := EventIndexerConfig{
		ContractAddresses: []string{contractAddress},
		EventSignatures:   []string{eventSignature},
	}
	return NewEventIndexerWithConfig(client, cfg, logger, wsURL...)
}

// NewEventIndexerWithConfig creates an event indexer with full filter configuration.
// This allows filtering by multiple contract addresses and event signatures.
func NewEventIndexerWithConfig(client EventReader, cfg EventIndexerConfig, logger *zap.Logger, wsURL ...string) (*EventIndexer, error) {
	ei := &EventIndexer{
		client:          client,
		logger:          logger,
		events:          make([]*IndexedEvent, 0, 1000),
		maxEvents:       10000,
		updateInterval:  15 * time.Second,
		stopChan:        make(chan struct{}),
		modeCh:          make(chan string, 1),
		mode:            "polling",
		seenIDs:         make(map[string]struct{}),
	}

	// Apply config
	if len(cfg.ContractAddresses) > 0 {
		ei.contractAddress = common.HexToAddress(cfg.ContractAddresses[0])
		for _, addr := range cfg.ContractAddresses {
			if addr != "" {
				ei.contractAddresses = append(ei.contractAddresses, common.HexToAddress(addr))
			}
		}
	}
	if len(cfg.EventSignatures) > 0 {
		ei.eventSignature = common.HexToHash(cfg.EventSignatures[0])
		for _, sig := range cfg.EventSignatures {
			if sig != "" {
				ei.eventSignatures = append(ei.eventSignatures, common.HexToHash(sig))
			}
		}
	}
	if cfg.StartBlock > 0 {
		ei.startBlock = cfg.StartBlock
	}
	if cfg.MaxEvents > 0 {
		ei.maxEvents = cfg.MaxEvents
	}
	if cfg.UpdateInterval > 0 {
		ei.updateInterval = cfg.UpdateInterval
	}
	ei.confirmationBlocks = cfg.ConfirmationBlocks
	if ei.confirmationBlocks == 0 {
		ei.confirmationBlocks = 12 // default: 12 blocks safety buffer
	}

	logger.Info("Creating event indexer",
		zap.Int("contract_count", len(ei.contractAddresses)),
		zap.Int("event_sig_count", len(ei.eventSignatures)))

	// Try to set up WebSocket subscriber if a WS URL is provided
	if len(wsURL) > 0 && wsURL[0] != "" {
		ei.subscriber = NewLogSubscriber(wsURL[0], logger)
		ei.mode = "websocket"
	}

	return ei, nil
}

// Start starts the event indexer. If a WebSocket subscriber is configured,
// it subscribes to real-time logs; otherwise falls back to polling.
func (ei *EventIndexer) Start(ctx context.Context) error {
	ei.logger.Info("Starting event indexer", zap.String("mode", ei.mode))

	// Get current block number
	blockNumber, err := ei.client.BlockNumber(ctx)
	if err != nil {
		ei.logger.Error("Failed to get current block number", zap.Error(err))
		return fmt.Errorf("failed to get current block number: %w", err)
	}

	ei.mu.Lock()
	// Try to load checkpoint from store for resume
	if ei.store != nil {
		if checkpoint, err := ei.store.LoadCheckpoint(ei.contractAddress.Hex()); err == nil && checkpoint > 0 {
			ei.startBlock = checkpoint
			ei.currentBlock = checkpoint
			ei.logger.Info("Resuming from checkpoint",
				zap.Uint64("checkpoint_block", checkpoint))
		} else {
			ei.startBlock = blockNumber
			ei.currentBlock = blockNumber
		}
	} else {
		ei.startBlock = blockNumber
		ei.currentBlock = blockNumber
	}
	ei.mu.Unlock()

	// Try WebSocket subscription first
	if ei.subscriber != nil {
		query := ethereum.FilterQuery{
			Addresses: []common.Address{ei.contractAddress},
			Topics:    [][]common.Hash{{ei.eventSignature}},
		}
		logs, err := ei.subscriber.Subscribe(ctx, query)
		if err != nil {
			ei.logger.Warn("WebSocket subscription failed, falling back to polling",
				zap.Error(err))
			ei.mode = "polling"
		} else {
			// Start WebSocket listener; indexingLoop handles both catch-up
			// polling and will switch to full polling if WS disconnects.
			ei.mode = "websocket"
			ei.wg.Add(2)
			go ei.websocketLoop(ctx, logs)
			go ei.indexingLoop(ctx)
			ei.logger.Info("Event indexer started with WebSocket",
				zap.Uint64("start_block", blockNumber))
			return nil
		}
	}

	// Fallback to polling only
	ei.wg.Add(1)
	go ei.indexingLoop(ctx)

	ei.logger.Info("Event indexer started with polling",
		zap.Uint64("start_block", blockNumber))
	return nil
}

// Stop stops the event indexer
func (ei *EventIndexer) Stop() {
	ei.logger.Info("Stopping event indexer")
	if ei.subscriber != nil {
		ei.subscriber.Unsubscribe()
	}
	ei.stopOnce.Do(func() {
		close(ei.stopChan)
	})
	ei.wg.Wait()
}

// SetEventStore sets the persistent event store for checkpointing and deduplication.
func (ei *EventIndexer) SetEventStore(store EventStore) {
	ei.mu.Lock()
	defer ei.mu.Unlock()
	ei.store = store
}

// SetReorgDetector sets the reorg detector for validating indexed events.
func (ei *EventIndexer) SetReorgDetector(rd *ReorgDetector) {
	ei.mu.Lock()
	defer ei.mu.Unlock()
	ei.reorgDetector = rd
}

// SetEventParser sets the event parser for decoding event data into the
// Decoded field of IndexedEvent. Without this, Decoded remains empty.
func (ei *EventIndexer) SetEventParser(ep *EventParser) {
	ei.mu.Lock()
	defer ei.mu.Unlock()
	ei.eventParser = ep
}

// websocketLoop processes real-time logs from a WebSocket subscription.
// When the subscription channel closes (e.g. WS disconnect), it signals
// the indexingLoop to switch to polling mode via modeCh.
func (ei *EventIndexer) websocketLoop(ctx context.Context, logs <-chan types.Log) {
	defer ei.wg.Done()
	for {
		select {
		case log, ok := <-logs:
			if !ok {
				ei.logger.Warn("WebSocket log channel closed, signalling switch to polling")
				// Signal the single indexingLoop to start polling.
				// Non-blocking send: indexingLoop will pick it up on next tick.
				select {
				case ei.modeCh <- "polling":
				default:
				}
				return
			}
			ei.processLog(ctx, log)
		case <-ei.stopChan:
			ei.logger.Info("WebSocket listener stopped")
			return
		case <-ctx.Done():
			ei.logger.Info("WebSocket listener cancelled")
			return
		}
	}
}

// processLog converts a raw types.Log into an IndexedEvent and stores it.
// It reuses logToEvent for consistent ID generation and EventParser decoding,
// and addEvent for deduplication and persistence.
// If a reorgDetector is configured, it verifies the block hash is still canonical
// before adding the event — events from reorg'd blocks are discarded.
func (ei *EventIndexer) processLog(ctx context.Context, log types.Log) {
	event := ei.logToEvent(&log)

	// Reorg check for WebSocket-sourced events
	ei.mu.RLock()
	rd := ei.reorgDetector
	ei.mu.RUnlock()

	if rd != nil && event.BlockHash != "" {
		reorged, err := rd.CheckReorg(ctx, event.BlockNumber, common.HexToHash(event.BlockHash))
		if err != nil {
			ei.logger.Warn("Reorg check failed for WebSocket event, skipping event",
				zap.String("tx_hash", event.TransactionHash),
				zap.Uint64("block", event.BlockNumber),
				zap.Error(err))
			return
		}
		if reorged {
			ei.logger.Warn("Discarding reorg'd WebSocket event",
				zap.String("tx_hash", event.TransactionHash),
				zap.Uint64("block", event.BlockNumber),
				zap.String("block_hash", event.BlockHash))
			return
		}
	}

	ei.addEvent(event)

	ei.logger.Debug("WebSocket event received",
		zap.String("tx_hash", event.TransactionHash),
		zap.Uint64("block", event.BlockNumber))
}

// indexingLoop continuously indexes events. In WebSocket mode it handles
// historical catch-up; if the WS disconnects, it receives a mode signal
// and switches to full polling.
func (ei *EventIndexer) indexingLoop(ctx context.Context) {
	defer ei.wg.Done()
	ticker := time.NewTicker(ei.updateInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			ei.indexEvents(ctx)
		case mode := <-ei.modeCh:
			ei.mu.Lock()
			ei.mode = mode
			ei.mu.Unlock()
			ei.logger.Info("Event indexer mode changed", zap.String("mode", mode))
			// Immediately start a polling cycle after mode switch
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

// indexEvents indexes events from the blockchain up to latestBlock - confirmationBlocks.
func (ei *EventIndexer) indexEvents(ctx context.Context) {
	ei.mu.RLock()
	currentBlock := ei.currentBlock
	confirmationBlocks := ei.confirmationBlocks
	ei.mu.RUnlock()

	// Get latest block
	latestBlock, err := ei.client.BlockNumber(ctx)
	if err != nil {
		ei.logger.Error("Failed to get latest block number", zap.Error(err))
		return
	}

	// Apply confirmation buffer: only index up to latestBlock - N
	safeBlock := latestBlock
	if confirmationBlocks > 0 && latestBlock > confirmationBlocks {
		safeBlock = latestBlock - confirmationBlocks
	}

	if safeBlock <= currentBlock {
		return
	}

	ei.indexRange(ctx, currentBlock+1, safeBlock)

	// Update current block to safeBlock (not latestBlock)
	ei.mu.Lock()
	ei.currentBlock = safeBlock
	store := ei.store
	reorgDetector := ei.reorgDetector
	ei.mu.Unlock()

	if store != nil {
		// Save checkpoint for resume after restart
		if err := store.SaveCheckpoint(ei.contractAddress.Hex(), safeBlock); err != nil {
			ei.logger.Error("Failed to save checkpoint", zap.Error(err))
		}

		// Run reorg detection on recently indexed events
		if reorgDetector != nil {
			recentEvents, err := store.GetEventsByBlockRange(currentBlock+1, safeBlock)
			if err == nil && len(recentEvents) > 0 {
				reorgedHashes := reorgDetector.MarkReorgedEvents(ctx, recentEvents)
				if len(reorgedHashes) > 0 {
					ei.logger.Warn("Reorg detected: marking events as reorged",
						zap.Int("reorged_count", len(reorgedHashes)))
					if count, err := store.MarkEventsReorged(reorgedHashes); err == nil {
						ei.logger.Info("Events marked as reorged", zap.Int("count", count))
					}
				}
			}
		}
	}
}

// indexRange indexes events in the specified block range [fromBlock, toBlock].
// This is the shared logic used by both indexEvents and Replay.
func (ei *EventIndexer) indexRange(ctx context.Context, fromBlock, toBlock uint64) {
	query := ethereum.FilterQuery{
		FromBlock: new(big.Int).SetUint64(fromBlock),
		ToBlock:   new(big.Int).SetUint64(toBlock),
	}
	if len(ei.contractAddresses) > 0 {
		query.Addresses = ei.contractAddresses
	}
	if len(ei.eventSignatures) > 0 {
		query.Topics = [][]common.Hash{ei.eventSignatures}
	}

	logs, err := ei.client.FilterLogs(ctx, query)
	if err != nil {
		ei.logger.Error("Failed to filter logs", zap.Error(err))
		return
	}

	ei.logger.Debug("Events indexed",
		zap.Int("count", len(logs)),
		zap.Uint64("from_block", fromBlock),
		zap.Uint64("to_block", toBlock))

	for _, log := range logs {
		event := ei.logToEvent(&log)
		ei.addEvent(event)
	}
}

// Replay re-indexes events in the given block range. This is useful for
// recovering from reorgs: roll back the checkpoint and re-process from a
// known safe block. Events already in the store with matching IDs are
// skipped by addEvent's dedup logic.
func (ei *EventIndexer) Replay(ctx context.Context, fromBlock, toBlock uint64) error {
	ei.logger.Info("Replaying events",
		zap.Uint64("from_block", fromBlock),
		zap.Uint64("to_block", toBlock))

	ei.indexRange(ctx, fromBlock, toBlock)

	// Roll back checkpoint so the next indexEvents resumes from the replay end
	ei.mu.Lock()
	if toBlock < ei.currentBlock {
		ei.currentBlock = toBlock
	}
	store := ei.store
	ei.mu.Unlock()

	if store != nil {
		if err := store.SaveCheckpoint(ei.contractAddress.Hex(), toBlock); err != nil {
			ei.logger.Error("Failed to save checkpoint after replay", zap.Error(err))
			return fmt.Errorf("failed to save checkpoint after replay: %w", err)
		}
	}

	return nil
}

// logToEvent converts a log to an indexed event
func (ei *EventIndexer) logToEvent(log *types.Log) *IndexedEvent {
	topics := make([]string, len(log.Topics))
	for i, topic := range log.Topics {
		topics[i] = topic.Hex()
	}

	decoded := make(map[string]interface{})

	// Use EventParser to decode the event if available
	ei.mu.RLock()
	parser := ei.eventParser
	ei.mu.RUnlock()

	if parser != nil {
		parsed := parser.ParseLogs([]*types.Log{log})
		if len(parsed) > 0 && parsed[0].Name != "Unknown" {
			decoded = parsed[0].Args
		}
	}

	event := &IndexedEvent{
		ID:              fmt.Sprintf("%s-%d", log.TxHash.Hex(), log.Index),
		EventType:       "ContractEvent",
		ContractAddress: log.Address.Hex(),
		TransactionHash: log.TxHash.Hex(),
		BlockNumber:     log.BlockNumber,
		BlockHash:       log.BlockHash.Hex(),
		LogIndex:        log.Index,
		Topics:          topics,
		Data:            fmt.Sprintf("0x%x", log.Data),
		Timestamp:       time.Now(),
		Decoded:         decoded,
	}

	return event
}

// addEvent adds an event to the index with deduplication and persistence.
func (ei *EventIndexer) addEvent(event *IndexedEvent) {
	ei.mu.Lock()
	defer ei.mu.Unlock()

	// Lazy-init seenIDs for in-memory dedup
	if ei.seenIDs == nil {
		ei.seenIDs = make(map[string]struct{})
	}

	// Deduplicate via EventStore if available
	if ei.store != nil {
		if exists, _ := ei.store.EventExists(event.ID); exists {
			ei.logger.Debug("Skipping duplicate event", zap.String("event_id", event.ID))
			return
		}
		if err := ei.store.SaveEvent(event); err != nil {
			ei.logger.Warn("Failed to persist event", zap.String("event_id", event.ID), zap.Error(err))
			// Continue with in-memory storage even if persistence fails
		}
	} else {
		// In-memory dedup when no EventStore
		if _, seen := ei.seenIDs[event.ID]; seen {
			ei.logger.Debug("Skipping duplicate event", zap.String("event_id", event.ID))
			return
		}
	}

	ei.events = append(ei.events, event)
	if ei.store == nil {
		ei.seenIDs[event.ID] = struct{}{}
	}

	// Keep only recent events in memory
	if len(ei.events) > ei.maxEvents {
		// Evict seen-set entries for trimmed events
		if ei.store == nil {
			for _, ev := range ei.events[:len(ei.events)-ei.maxEvents] {
				delete(ei.seenIDs, ev.ID)
			}
		}
		// Compact to a fresh slice to release old backing array
		trimmed := make([]*IndexedEvent, ei.maxEvents)
		copy(trimmed, ei.events[len(ei.events)-ei.maxEvents:])
		ei.events = trimmed
	}
}

// isReorged checks if an event has been marked as reorg'd.
func isReorged(event *IndexedEvent) bool {
	if event.Decoded == nil {
		return false
	}
	reorged, ok := event.Decoded["reorged"].(bool)
	return ok && reorged
}

// GetEvents returns all indexed events, excluding reorg'd ones.
func (ei *EventIndexer) GetEvents() []*IndexedEvent {
	ei.mu.RLock()
	defer ei.mu.RUnlock()

	result := make([]*IndexedEvent, 0, len(ei.events))
	for _, event := range ei.events {
		if !isReorged(event) {
			result = append(result, event)
		}
	}
	return result
}

// GetEventsByType returns events of a specific type, excluding reorg'd ones.
func (ei *EventIndexer) GetEventsByType(eventType string) []*IndexedEvent {
	ei.mu.RLock()
	defer ei.mu.RUnlock()

	result := make([]*IndexedEvent, 0)
	for _, event := range ei.events {
		if event.EventType == eventType && !isReorged(event) {
			result = append(result, event)
		}
	}

	return result
}

// GetEventsByBlockRange returns events in a block range, excluding reorg'd ones.
func (ei *EventIndexer) GetEventsByBlockRange(fromBlock, toBlock uint64) []*IndexedEvent {
	ei.mu.RLock()
	defer ei.mu.RUnlock()

	result := make([]*IndexedEvent, 0)
	for _, event := range ei.events {
		if event.BlockNumber >= fromBlock && event.BlockNumber <= toBlock && !isReorged(event) {
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
			el.logger.Error("Error handling event",
				zap.String("event_type", event.EventType),
				zap.Error(err))
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
			el.logger.Error("Error processing event",
				zap.String("event_id", event.ID),
				zap.Error(err))
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
	if len(event.Topics) < 3 {
		return nil, fmt.Errorf("ContentRegisteredEvent: expected at least 3 topics, got %d", len(event.Topics))
	}
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
	if len(event.Topics) < 4 {
		return nil, fmt.Errorf("NFTMintedEvent: expected at least 4 topics, got %d", len(event.Topics))
	}
	return &NFTMintedEvent{
		TokenID: event.Topics[3],
		Owner:   event.Topics[2],
	}, nil
}
