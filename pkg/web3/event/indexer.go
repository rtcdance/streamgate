package event

import (
	"context"
	"fmt"
	"math/big"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/rtcdance/streamgate/pkg/monitoring"
	"go.uber.org/zap"
)

type LogSubscriberInterface interface {
	Subscribe(ctx context.Context, query ethereum.FilterQuery) (<-chan types.Log, error)
	Unsubscribe()
}

type ReorgChecker interface {
	CheckReorg(ctx context.Context, blockNumber uint64, blockHash common.Hash) (bool, error)
	MarkReorgedEvents(ctx context.Context, events []*IndexedEvent) []string
}

type EventReader interface {
	BlockNumber(ctx context.Context) (uint64, error)
	FilterLogs(ctx context.Context, q ethereum.FilterQuery) ([]types.Log, error)
}

//go:generate mockgen -destination=../mocks/mock_event_reader.go -package=mocks streamgate/pkg/web3/event EventReader

type EventIndexerConfig struct {
	ContractAddresses  []string
	EventSignatures    []string
	StartBlock         uint64
	MaxEvents          int
	UpdateInterval     time.Duration
	ConfirmationBlocks uint64
	Finality           string
	ChainID            int64
	FilterVersion      string
	// AllowUnfiltered is a test-only backdoor: production Start refuses to
	// index without a contract whitelist (full-chain scanning is forbidden).
	AllowUnfiltered bool
}

type EventIndexer struct {
	client             EventReader
	logger             *zap.Logger
	contractAddress    common.Address
	eventSignature     common.Hash
	contractAddresses  []common.Address
	eventSignatures    []common.Hash
	startBlock         uint64
	currentBlock       uint64
	mu                 sync.RWMutex
	events             []*IndexedEvent
	maxEvents          int
	updateInterval     time.Duration
	stopChan           chan struct{}
	modeCh             chan string
	subscriber         LogSubscriberInterface
	mode               string
	store              EventStore
	reorgDetector      ReorgChecker
	stopOnce           sync.Once
	wg                 sync.WaitGroup
	eventParser        *EventParser
	seenIDs            map[string]struct{}
	confirmationBlocks uint64
	onEvent            EventHandler
	started            atomic.Bool
	wsBackoff          time.Duration
	chainID            int64
	filterVersion      string
	allowUnfiltered    bool
	paused             atomic.Bool
}

type IndexedEvent struct {
	ID              string
	ChainID         int64
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

func NewEventIndexer(client EventReader, contractAddress, eventSignature string, logger *zap.Logger) (*EventIndexer, error) {
	cfg := EventIndexerConfig{
		ContractAddresses: []string{contractAddress},
		EventSignatures:   []string{eventSignature},
	}
	return NewEventIndexerWithConfig(client, cfg, logger)
}

func NewEventIndexerWithConfig(client EventReader, cfg EventIndexerConfig, logger *zap.Logger) (*EventIndexer, error) {
	ei := &EventIndexer{
		client:         client,
		logger:         logger,
		events:         make([]*IndexedEvent, 0, 1000),
		maxEvents:      10000,
		updateInterval: 15 * time.Second,
		stopChan:       make(chan struct{}),
		modeCh:         make(chan string, 1),
		mode:           "polling",
		seenIDs:        make(map[string]struct{}),
		wsBackoff:      time.Second,
	}

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
		ei.confirmationBlocks = 12
	}
	ei.chainID = cfg.ChainID
	ei.filterVersion = cfg.FilterVersion
	ei.allowUnfiltered = cfg.AllowUnfiltered

	logger.Info("Creating event indexer",
		zap.Int("contract_count", len(ei.contractAddresses)),
		zap.Int("event_sig_count", len(ei.eventSignatures)))

	return ei, nil
}

func (ei *EventIndexer) SetSubscriber(sub LogSubscriberInterface) {
	ei.mu.Lock()
	defer ei.mu.Unlock()
	ei.subscriber = sub
	ei.mode = "websocket"
}

func (ei *EventIndexer) Start(ctx context.Context) error {
	if !ei.started.CompareAndSwap(false, true) {
		ei.logger.Warn("Event indexer already started, ignoring duplicate call")
		return nil
	}

	if len(ei.contractAddresses) == 0 && !ei.allowUnfiltered {
		ei.started.Store(false)
		return fmt.Errorf("event indexer refuses to start without a contract whitelist")
	}

	ei.stopChan = make(chan struct{})
	ei.stopOnce = sync.Once{}
	ei.modeCh = make(chan string, 1)

	ei.logger.Info("Starting event indexer", zap.String("mode", ei.mode))

	blockNumber, err := ei.client.BlockNumber(ctx)
	if err != nil {
		ei.logger.Error("Failed to get current block number", zap.Error(err))
		return fmt.Errorf("failed to get current block number: %w", err)
	}

	ei.mu.Lock()
	if ei.store != nil {
		if ds, ok := ei.store.(DurableEventStore); ok {
			if checkpoint, err := ds.LoadScopedCheckpoint(ctx, ei.chainID, ei.filterKey()); err == nil && checkpoint > 0 {
				ei.startBlock = checkpoint
				ei.currentBlock = checkpoint
				ei.logger.Info("Resuming from scoped checkpoint",
					zap.Uint64("checkpoint_block", checkpoint),
					zap.String("filter_key", ei.filterKey()))
			} else if ei.startBlock > 0 {
				ei.currentBlock = ei.startBlock
				ei.logger.Info("Starting from configured startBlock",
					zap.Uint64("start_block", ei.startBlock))
			} else {
				ei.startBlock = blockNumber
				ei.currentBlock = blockNumber
			}
		} else if checkpoint, err := ei.store.LoadCheckpoint(ei.contractAddress.Hex()); err == nil && checkpoint > 0 {
			ei.startBlock = checkpoint
			ei.currentBlock = checkpoint
			ei.logger.Info("Resuming from checkpoint",
				zap.Uint64("checkpoint_block", checkpoint))
		} else if ei.startBlock > 0 {
			ei.currentBlock = ei.startBlock
			ei.logger.Info("Starting from configured startBlock",
				zap.Uint64("start_block", ei.startBlock))
		} else {
			ei.startBlock = blockNumber
			ei.currentBlock = blockNumber
		}
	} else if ei.startBlock > 0 {
		ei.currentBlock = ei.startBlock
		ei.logger.Info("Starting from configured startBlock",
			zap.Uint64("start_block", ei.startBlock))
	} else {
		ei.startBlock = blockNumber
		ei.currentBlock = blockNumber
	}
	ei.mu.Unlock()

	ei.mu.RLock()
	subscriber := ei.subscriber
	ei.mu.RUnlock()

	if subscriber != nil {
		query := ei.buildFilterQuery()
		logs, err := subscriber.Subscribe(ctx, query)
		if err != nil {
			ei.logger.Warn("WebSocket subscription failed, falling back to polling",
				zap.Error(err))
			ei.mode = "polling"
		} else {
			ei.mode = "websocket"
			ei.wg.Add(2)
			go ei.websocketLoop(ctx, logs)
			go ei.indexingLoop(ctx)
			ei.logger.Info("Event indexer started with WebSocket",
				zap.Uint64("start_block", blockNumber))
			return nil
		}
	}

	ei.wg.Add(1)
	go ei.indexingLoop(ctx)

	ei.logger.Info("Event indexer started with polling",
		zap.Uint64("start_block", blockNumber))
	return nil
}

func (ei *EventIndexer) Stop() {
	ei.logger.Info("Stopping event indexer")
	ei.mu.RLock()
	sub := ei.subscriber
	ei.mu.RUnlock()
	if sub != nil {
		sub.Unsubscribe()
	}
	ei.stopOnce.Do(func() {
		close(ei.stopChan)
	})
	ei.wg.Wait()
	ei.started.Store(false)
}

func (ei *EventIndexer) SetEventStore(store EventStore) {
	ei.mu.Lock()
	defer ei.mu.Unlock()
	ei.store = store
}

func (ei *EventIndexer) SetReorgDetector(rd ReorgChecker) {
	ei.mu.Lock()
	defer ei.mu.Unlock()
	ei.reorgDetector = rd
}

func (ei *EventIndexer) SetEventParser(ep *EventParser) {
	ei.mu.Lock()
	defer ei.mu.Unlock()
	ei.eventParser = ep
}

func (ei *EventIndexer) SetOnEvent(handler EventHandler) {
	ei.mu.Lock()
	defer ei.mu.Unlock()
	ei.onEvent = handler
}

func (ei *EventIndexer) SetFilterVersion(version string) {
	ei.mu.Lock()
	defer ei.mu.Unlock()
	ei.filterVersion = version
}

// filterKey scopes durable checkpoints by (filter version, contract
// whitelist): changing either yields a new key, so a checkpoint written under
// one filter is never reused by another.
func (ei *EventIndexer) filterKey() string {
	addresses := make([]string, 0, len(ei.contractAddresses))
	for _, addr := range ei.contractAddresses {
		addresses = append(addresses, strings.ToLower(addr.Hex()))
	}
	sort.Strings(addresses)
	return ei.filterVersion + "|" + strings.Join(addresses, ",")
}

// buildFilterQuery returns the one filter used everywhere (Start, polling
// batches, reconnect) so no path can drop the contract whitelist.
func (ei *EventIndexer) buildFilterQuery() ethereum.FilterQuery {
	query := ethereum.FilterQuery{}
	if len(ei.contractAddresses) > 0 {
		query.Addresses = ei.contractAddresses
	} else if ei.contractAddress != (common.Address{}) {
		query.Addresses = []common.Address{ei.contractAddress}
	}
	if len(ei.eventSignatures) > 0 {
		query.Topics = [][]common.Hash{ei.eventSignatures}
	} else if ei.eventSignature != (common.Hash{}) {
		query.Topics = [][]common.Hash{{ei.eventSignature}}
	}
	return query
}

func (ei *EventIndexer) websocketLoop(ctx context.Context, logs <-chan types.Log) {
	defer ei.wg.Done()
	for {
		select {
		case log, ok := <-logs:
			if !ok {
				ei.mu.RLock()
				sub := ei.subscriber
				ei.mu.RUnlock()
				if sub != nil {
					newLogs, reconnected := ei.reconnectWithBackoff(ctx)
					if reconnected {
						logs = newLogs
						continue
					}
				}
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

func (ei *EventIndexer) reconnectWithBackoff(ctx context.Context) (<-chan types.Log, bool) {
	for {
		select {
		case <-ctx.Done():
			return nil, false
		case <-ei.stopChan:
			return nil, false
		case <-time.After(ei.wsBackoff):
		}

		ei.mu.RLock()
		sub := ei.subscriber
		ei.mu.RUnlock()

		if sub == nil {
			return nil, false
		}

		sub.Unsubscribe()

		logs, err := sub.Subscribe(ctx, ei.buildFilterQuery())
		if err == nil {
			ei.mu.Lock()
			ei.wsBackoff = time.Second
			ei.mu.Unlock()
			ei.logger.Info("WebSocket reconnected")
			return logs, true
		}

		ei.logger.Warn("WebSocket reconnect failed",
			zap.Duration("backoff", ei.wsBackoff),
			zap.Error(err))

		ei.mu.Lock()
		ei.wsBackoff *= 2
		if ei.wsBackoff > 30*time.Second {
			ei.wsBackoff = 30 * time.Second
		}
		ei.mu.Unlock()
	}
}

func (ei *EventIndexer) processLog(ctx context.Context, log types.Log) {
	event := ei.logToEvent(&log)

	ei.mu.RLock()
	rd := ei.reorgDetector
	ei.mu.RUnlock()

	if rd != nil && event.BlockHash != "" && event.BlockHash != "0x0000000000000000000000000000000000000000000000000000000000000000" {
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

	storeCtx, storeCancel := context.WithTimeout(context.Background(), 10*time.Second)
	ei.addEvent(storeCtx, event)
	storeCancel()

	ei.logger.Debug("WebSocket event received",
		zap.String("tx_hash", event.TransactionHash),
		zap.Uint64("block", event.BlockNumber))
}

func (ei *EventIndexer) indexingLoop(ctx context.Context) {
	defer ei.wg.Done()
	ticker := time.NewTicker(ei.updateInterval)
	defer ticker.Stop()

	consecutiveFailures := 0
	maxBackoff := 5 * time.Minute

	for {
		select {
		case <-ticker.C:
			err := ei.indexEvents(ctx)
			if err != nil {
				consecutiveFailures++
				backoff := ei.updateInterval * time.Duration(1<<min(consecutiveFailures, 8))
				if backoff > maxBackoff {
					backoff = maxBackoff
				}
				if consecutiveFailures <= 2 {
					ei.logger.Warn("Event indexer backing off",
						zap.Int("failures", consecutiveFailures),
						zap.Duration("backoff", backoff))
				}
				ticker.Reset(backoff)
			} else {
				if consecutiveFailures > 0 {
					ei.logger.Info("Event indexer recovered",
						zap.Int("previous_failures", consecutiveFailures))
					consecutiveFailures = 0
					ticker.Reset(ei.updateInterval)
				}
			}
		case mode := <-ei.modeCh:
			ei.mu.Lock()
			ei.mode = mode
			ei.mu.Unlock()
			ei.logger.Info("Event indexer mode changed", zap.String("mode", mode))
			if err := ei.indexEvents(ctx); err != nil {
				ei.logger.Warn("Index events failed", zap.Error(err))
			}
		case <-ei.stopChan:
			ei.logger.Info("Event indexing loop stopped")
			return
		case <-ctx.Done():
			ei.logger.Info("Event indexing loop cancelled")
			return
		}
	}
}

func (ei *EventIndexer) indexEvents(ctx context.Context) error {
	ei.mu.RLock()
	currentBlock := ei.currentBlock
	confirmationBlocks := ei.confirmationBlocks
	ei.mu.RUnlock()

	latestBlock, err := ei.client.BlockNumber(ctx)
	if err != nil {
		ei.logger.Error("Failed to get latest block number", zap.Error(err))
		return err
	}

	safeBlock := latestBlock
	if confirmationBlocks > 0 && latestBlock > confirmationBlocks {
		safeBlock = latestBlock - confirmationBlocks
	}

	if safeBlock <= currentBlock {
		return nil
	}

	if err := ei.indexRange(ctx, currentBlock+1, safeBlock); err != nil {
		return err
	}

	ei.mu.RLock()
	store := ei.store
	reorgDetector := ei.reorgDetector
	ei.mu.RUnlock()

	if store != nil && reorgDetector != nil {
		recentEvents, err := store.GetEventsByBlockRange(currentBlock+1, safeBlock)
		if err == nil && len(recentEvents) > 0 {
			reorgedHashes := reorgDetector.MarkReorgedEvents(ctx, recentEvents)
			if len(reorgedHashes) > 0 {
				ei.logger.Warn("Reorg detected: marking events as reorged",
					zap.Int("reorged_count", len(reorgedHashes)))
				if count, err := store.MarkEventsReorged(reorgedHashes); err == nil {
					ei.logger.Info("Events marked as reorged", zap.Int("count", count))
				}
				monitoring.EventIndexerReorgsTotal.Inc()
				if ds, ok := store.(DurableEventStore); ok {
					// Prefer the journal: it rolls back to the common ancestor
					// instead of guessing from event transactions, and it fails
					// loudly when the reorg is deeper than the journal.
					if err := ei.rollbackToCommonAncestor(ctx, ds); err != nil {
						ei.paused.Store(true)
						return err
					}
					ei.paused.Store(false)
					return nil
				}
				minBlock := uint64(0)
				for _, ev := range recentEvents {
					for _, h := range reorgedHashes {
						if ev.TransactionHash == h && (minBlock == 0 || ev.BlockNumber < minBlock) {
							minBlock = ev.BlockNumber
						}
					}
				}
				if minBlock > 0 {
					ei.mu.Lock()
					if minBlock-1 < ei.currentBlock {
						ei.currentBlock = minBlock - 1
					}
					cb := ei.currentBlock
					ei.mu.Unlock()
					if err := store.SaveCheckpoint(ei.contractAddress.Hex(), cb); err != nil {
						ei.logger.Error("Failed to save reorg checkpoint",
							zap.Uint64("block", cb), zap.Error(err))
					}
					ei.logger.Info("Checkpoint rolled back after reorg",
						zap.Uint64("to_block", ei.currentBlock))
				}
			}
		}
	}

	return nil
}

const indexBatchSize uint64 = 100

// journalDepth bounds how far back the block-hash journal can prove a common
// ancestor; a deeper reorg must surface as an error, never as silent success.
const journalDepth = 64

func (ei *EventIndexer) indexRange(ctx context.Context, fromBlock, toBlock uint64) error {
	start := time.Now()
	for batchStart := fromBlock; batchStart <= toBlock; {
		select {
		case <-ctx.Done():
			ei.logger.Info("indexRange cancelled", zap.Uint64("last_batch_start", batchStart))
			return ctx.Err()
		default:
		}

		batchEnd := batchStart + indexBatchSize - 1
		if batchEnd > toBlock {
			batchEnd = toBlock
		}

		query := ei.buildFilterQuery()
		query.FromBlock = new(big.Int).SetUint64(batchStart)
		query.ToBlock = new(big.Int).SetUint64(batchEnd)

		logs, err := ei.client.FilterLogs(ctx, query)
		if err != nil {
			ei.logger.Error("Failed to filter logs",
				zap.Uint64("from_block", batchStart),
				zap.Uint64("to_block", batchEnd),
				zap.Error(err))
			monitoring.EventIndexerEventsTotal.WithLabelValues(ei.contractAddress.Hex(), "filter", "error").Inc()
			return err
		}

		ei.mu.RLock()
		store := ei.store
		chainID := ei.chainID
		ei.mu.RUnlock()

		events := make([]*IndexedEvent, 0, len(logs))
		for i := range logs {
			events = append(events, ei.logToEvent(&logs[i]))
		}

		// The batch must be all-or-nothing: persist events AND checkpoint
		// together, and only advance the cursor / dispatch events after the
		// commit succeeded — a failed batch can never move the cursor forward.
		if ds, ok := store.(DurableEventStore); ok {
			endBlockHash := ""
			if len(logs) > 0 {
				endBlockHash = logs[len(logs)-1].BlockHash.Hex()
			}
			if err := ds.SaveBatch(ctx, chainID, ei.filterKey(), events, batchEnd, endBlockHash); err != nil {
				ei.logger.Error("Failed to commit batch, cursor not advanced",
					zap.Uint64("from_block", batchStart),
					zap.Uint64("to_block", batchEnd),
					zap.Error(err))
				return err
			}
			if err := ds.SaveBlockJournal(ctx, chainID, journalBlocks(logs), journalDepth); err != nil {
				ei.logger.Warn("Failed to record block journal", zap.Error(err))
			}
		} else {
			for _, event := range events {
				if store == nil {
					continue
				}
				if exists, err := store.EventExists(event.ID); err == nil && exists {
					continue
				}
				if err := store.SaveEvent(event); err != nil {
					ei.logger.Error("Failed to persist event, cursor not advanced",
						zap.String("event_id", event.ID), zap.Error(err))
					return err
				}
			}
			if store != nil {
				if err := store.SaveCheckpoint(ei.contractAddress.Hex(), batchEnd); err != nil {
					ei.logger.Warn("Failed to save checkpoint", zap.Uint64("block", batchEnd), zap.Error(err))
				}
			}
		}

		ei.mu.Lock()
		ei.currentBlock = batchEnd
		ei.mu.Unlock()

		for _, event := range events {
			ei.dispatchEvent(ctx, event)
			monitoring.EventIndexerEventsTotal.WithLabelValues(event.ContractAddress, event.EventType, "success").Inc()
		}

		monitoring.EventIndexerCurrentBlock.Set(float64(batchEnd))

		ei.logger.Debug("Batch indexed",
			zap.Int("count", len(logs)),
			zap.Uint64("from_block", batchStart),
			zap.Uint64("to_block", batchEnd))

		batchStart = batchEnd + 1
	}
	monitoring.EventIndexerIndexDuration.WithLabelValues("batch").Observe(time.Since(start).Seconds())
	return nil
}

// journalBlocks dedupes the journaled hashes of a batch's logs by block
// number, ascending.
func journalBlocks(logs []types.Log) []JournalBlock {
	byNumber := make(map[uint64]string, len(logs))
	for _, l := range logs {
		if _, ok := byNumber[l.BlockNumber]; !ok {
			byNumber[l.BlockNumber] = l.BlockHash.Hex()
		}
	}
	blocks := make([]JournalBlock, 0, len(byNumber))
	for n, h := range byNumber {
		blocks = append(blocks, JournalBlock{Number: n, Hash: h})
	}
	sort.Slice(blocks, func(i, j int) bool { return blocks[i].Number < blocks[j].Number })
	return blocks
}

// rollbackToCommonAncestor walks the journal newest-to-oldest until a block
// hash still matches the chain: everything above it is orphaned, the scoped
// checkpoint and cursor rewind to it. A reorg deeper than the journal cannot
// be proven safe, so it returns an error instead of pretending to recover.
func (ei *EventIndexer) rollbackToCommonAncestor(ctx context.Context, ds DurableEventStore) error {
	ei.mu.RLock()
	currentBlock := ei.currentBlock
	chainID := ei.chainID
	filterKey := ei.filterKey()
	rd := ei.reorgDetector
	ei.mu.RUnlock()
	if rd == nil {
		return fmt.Errorf("rollback requested without a reorg detector")
	}

	fromBlock := uint64(0)
	if currentBlock >= journalDepth {
		fromBlock = currentBlock - journalDepth
	}
	journal, err := ds.LoadBlockJournal(ctx, chainID, fromBlock)
	if err != nil {
		return fmt.Errorf("load block journal: %w", err)
	}
	if len(journal) == 0 {
		return fmt.Errorf("reorg deeper than journal depth %d", journalDepth)
	}

	var orphanHashes []string
	var ancestor *JournalBlock
	for i := len(journal) - 1; i >= 0; i-- {
		b := journal[i]
		reorged, err := rd.CheckReorg(ctx, b.Number, common.HexToHash(b.Hash))
		if err != nil {
			return fmt.Errorf("reorg check block %d: %w", b.Number, err)
		}
		if reorged {
			orphanHashes = append(orphanHashes, b.Hash)
			continue
		}
		ancestor = &b
		break
	}
	if ancestor == nil {
		return fmt.Errorf("reorg deeper than journal depth %d", journalDepth)
	}

	if _, err := ds.MarkEventsReorged(orphanHashes); err != nil {
		return fmt.Errorf("mark reorged events: %w", err)
	}
	if err := ds.MarkJournalOrphaned(ctx, chainID, ancestor.Number); err != nil {
		return fmt.Errorf("mark journal orphaned: %w", err)
	}
	if err := ds.RollbackScopedCheckpoint(ctx, chainID, filterKey, ancestor.Number); err != nil {
		return fmt.Errorf("rollback scoped checkpoint: %w", err)
	}
	if err := ds.SaveCheckpoint(ei.contractAddress.Hex(), ancestor.Number); err != nil {
		ei.logger.Warn("Failed to roll back legacy checkpoint", zap.Error(err))
	}

	ei.mu.Lock()
	ei.currentBlock = ancestor.Number
	ei.mu.Unlock()

	monitoring.EventIndexerCurrentBlock.Set(float64(ancestor.Number))
	ei.logger.Info("Rolled back to common ancestor",
		zap.Uint64("ancestor_block", ancestor.Number),
		zap.Int("orphaned_blocks", len(orphanHashes)))
	return nil
}

// UpdateFilter swaps the contract whitelist: checkpoints become bounded by
// the new filter key (catch-up continues from the old checkpoint at most), and
// a WebSocket subscriber re-subscribes with the new query.
func (ei *EventIndexer) UpdateFilter(ctx context.Context, newAddresses []string) error {
	seen := make(map[string]struct{}, len(newAddresses))
	normalized := make([]string, 0, len(newAddresses))
	for _, addr := range newAddresses {
		if addr == "" {
			continue
		}
		lower := strings.ToLower(addr)
		if _, ok := seen[lower]; ok {
			continue
		}
		seen[lower] = struct{}{}
		normalized = append(normalized, lower)
	}
	if len(normalized) == 0 && !ei.allowUnfiltered {
		return fmt.Errorf("event indexer refuses an empty contract whitelist")
	}
	sort.Strings(normalized)

	newAddrs := make([]common.Address, 0, len(normalized))
	for _, a := range normalized {
		newAddrs = append(newAddrs, common.HexToAddress(a))
	}

	ei.mu.Lock()
	oldKey := ei.filterKey()
	oldContractAddress := ei.contractAddress
	addresses := newAddrs
	if len(addresses) > 0 {
		ei.contractAddress = addresses[0]
	} else {
		ei.contractAddress = common.Address{}
	}
	ei.contractAddresses = addresses
	newKey := ei.filterKey()
	store := ei.store
	subscriber := ei.subscriber
	ei.mu.Unlock()

	if ds, ok := store.(DurableEventStore); ok {
		target, err := ds.LoadScopedCheckpoint(ctx, ei.chainID, newKey)
		if err != nil {
			return fmt.Errorf("load new filter checkpoint: %w", err)
		}
		if target == 0 {
			if legacy, err := store.LoadCheckpoint(oldContractAddress.Hex()); err == nil {
				target = legacy
			}
		}
		ei.mu.Lock()
		if target < ei.currentBlock {
			ei.currentBlock = target
		}
		ei.logger.Info("Filter updated",
			zap.String("old_filter_key", oldKey),
			zap.String("new_filter_key", newKey),
			zap.Uint64("catch_up_from", target))
		ei.mu.Unlock()
	} else if store != nil {
		if legacy, err := store.LoadCheckpoint(oldContractAddress.Hex()); err == nil && legacy > 0 {
			if err := store.SaveCheckpoint(ei.contractAddress.Hex(), legacy); err != nil {
				ei.logger.Warn("Failed to copy checkpoint to new filter", zap.Error(err))
			}
		}
		ei.mu.Lock()
		ei.logger.Info("Filter updated", zap.String("old_filter_key", oldKey), zap.String("new_filter_key", newKey))
		ei.mu.Unlock()
	}

	if subscriber != nil {
		query := ei.buildFilterQuery()
		subscriber.Unsubscribe()
		if logs, err := subscriber.Subscribe(ctx, query); err != nil {
			// Resubscription is best-effort: the polling loop keeps indexing
			// under the new filter either way.
			ei.logger.Warn("Re-subscribe after filter update failed", zap.Error(err))
		} else {
			ei.wg.Add(1)
			go ei.websocketLoop(ctx, logs)
		}
	}
	return nil
}

func (ei *EventIndexer) Replay(ctx context.Context, fromBlock, toBlock uint64) error {
	ei.logger.Info("Replaying events",
		zap.Uint64("from_block", fromBlock),
		zap.Uint64("to_block", toBlock))

	if err := ei.indexRange(ctx, fromBlock, toBlock); err != nil {
		return err
	}

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

func (ei *EventIndexer) logToEvent(log *types.Log) *IndexedEvent {
	topics := make([]string, len(log.Topics))
	for i, topic := range log.Topics {
		topics[i] = topic.Hex()
	}

	decoded := make(map[string]interface{})

	ei.mu.RLock()
	parser := ei.eventParser
	chainID := ei.chainID
	ei.mu.RUnlock()

	if parser != nil {
		parsed := parser.ParseLogs([]*types.Log{log})
		if len(parsed) > 0 && parsed[0].Name != "Unknown" {
			decoded = parsed[0].Args
		}
	}

	event := &IndexedEvent{
		ID:              fmt.Sprintf("%d-%s-%s-%d", chainID, log.BlockHash.Hex(), log.TxHash.Hex(), log.Index),
		ChainID:         chainID,
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

func (ei *EventIndexer) addEvent(ctx context.Context, event *IndexedEvent) {
	ei.mu.Lock()

	if ei.seenIDs == nil {
		ei.seenIDs = make(map[string]struct{})
	}

	if _, seen := ei.seenIDs[event.ID]; seen {
		ei.mu.Unlock()
		ei.logger.Debug("Skipping duplicate event", zap.String("event_id", event.ID))
		return
	}

	store := ei.store
	ei.mu.Unlock()

	if store != nil {
		if exists, _ := store.EventExists(event.ID); exists {
			ei.mu.Lock()
			ei.seenIDs[event.ID] = struct{}{}
			ei.mu.Unlock()
			ei.logger.Debug("Skipping duplicate event (persisted)", zap.String("event_id", event.ID))
			return
		}
		if err := store.SaveEvent(event); err != nil {
			ei.logger.Warn("Failed to persist event", zap.String("event_id", event.ID), zap.Error(err))
		}
	}

	ei.dispatchEvent(ctx, event)
}

// dispatchEvent appends the event to the in-memory window and hands it to the
// onEvent handler. Batch indexing calls it only AFTER the batch committed, so
// a failed batch never reaches consumers.
func (ei *EventIndexer) dispatchEvent(ctx context.Context, event *IndexedEvent) {
	ei.mu.Lock()

	if ei.seenIDs == nil {
		ei.seenIDs = make(map[string]struct{})
	}

	if _, seen := ei.seenIDs[event.ID]; seen {
		ei.mu.Unlock()
		ei.logger.Debug("Skipping duplicate event", zap.String("event_id", event.ID))
		return
	}

	ei.events = append(ei.events, event)
	ei.seenIDs[event.ID] = struct{}{}

	if len(ei.seenIDs) > ei.maxEvents*2 {
		ei.seenIDs = make(map[string]struct{}, ei.maxEvents)
		for _, ev := range ei.events {
			ei.seenIDs[ev.ID] = struct{}{}
		}
	}

	if len(ei.events) > ei.maxEvents {
		for _, ev := range ei.events[:len(ei.events)-ei.maxEvents] {
			delete(ei.seenIDs, ev.ID)
		}
		trimmed := make([]*IndexedEvent, ei.maxEvents)
		copy(trimmed, ei.events[len(ei.events)-ei.maxEvents:])
		ei.events = trimmed
	}

	onEvent := ei.onEvent
	ei.mu.Unlock()

	if onEvent != nil {
		_ = onEvent(ctx, event)
	}
}

func isReorged(event *IndexedEvent) bool {
	if event.Decoded == nil {
		return false
	}
	reorged, ok := event.Decoded["reorged"].(bool)
	return ok && reorged
}

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

func (ei *EventIndexer) GetEventCount() int {
	ei.mu.RLock()
	defer ei.mu.RUnlock()

	return len(ei.events)
}

func (ei *EventIndexer) GetCurrentBlock() uint64 {
	ei.mu.RLock()
	defer ei.mu.RUnlock()

	return ei.currentBlock
}

func (ei *EventIndexer) GetReorgSafeBlock(ctx context.Context) (uint64, error) {
	currentBlock, err := ei.client.BlockNumber(ctx)
	if err != nil {
		ei.logger.Error("Failed to get current block number", zap.Error(err))
		return 0, fmt.Errorf("failed to get current block number: %w", err)
	}

	if currentBlock <= ei.confirmationBlocks {
		ei.logger.Warn("Current block number is less than confirmation blocks, returning genesis",
			zap.Uint64("current_block", currentBlock),
			zap.Uint64("confirmation_blocks", ei.confirmationBlocks))
		return 0, nil
	}

	safeBlock := currentBlock - ei.confirmationBlocks

	ei.logger.Debug("Retrieved reorg-safe block",
		zap.Uint64("current_block", currentBlock),
		zap.Uint64("safe_block", safeBlock),
		zap.Uint64("confirmation_blocks", ei.confirmationBlocks))

	return safeBlock, nil
}

type EventListener struct {
	indexer  *EventIndexer
	logger   *zap.Logger
	handlers map[string][]EventHandler
	mu       sync.RWMutex
}

type EventHandler func(ctx context.Context, event *IndexedEvent) error

func NewEventListener(indexer *EventIndexer, logger *zap.Logger) *EventListener {
	return &EventListener{
		indexer:  indexer,
		logger:   logger,
		handlers: make(map[string][]EventHandler),
	}
}

func (el *EventListener) On(eventType string, handler EventHandler) {
	el.mu.Lock()
	defer el.mu.Unlock()

	el.handlers[eventType] = append(el.handlers[eventType], handler)
	el.logger.Debug("Event handler registered", zap.String("event_type", eventType))
}

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

// ContentRegisteredEvent carries only what the indexed topics can prove.
//
// ContentRegistry emits ContentRegistered(bytes32 indexed contentKey,
// string contentId, address indexed creator, bytes32 contentDigest, ...).
// A dynamic `string` cannot be recovered from an indexed topic, so contentId is
// deliberately absent here — callers that need it must decode the data payload
// against the ABI. ContentKey is keccak256(contentId), NOT the contentId.
type ContentRegisteredEvent struct {
	ContentKey string
	Creator    string
}

func DecodeContentRegisteredEvent(event *IndexedEvent) (*ContentRegisteredEvent, error) {
	if len(event.Topics) < 3 {
		return nil, fmt.Errorf("ContentRegisteredEvent: expected at least 3 topics, got %d", len(event.Topics))
	}
	return &ContentRegisteredEvent{
		ContentKey: event.Topics[1],
		Creator:    event.Topics[2],
	}, nil
}

type NFTMintedEvent struct {
	TokenID string
	Owner   string
	URI     string
}

func DecodeNFTMintedEvent(event *IndexedEvent) (*NFTMintedEvent, error) {
	if len(event.Topics) < 4 {
		return nil, fmt.Errorf("NFTMintedEvent: expected at least 4 topics, got %d", len(event.Topics))
	}
	return &NFTMintedEvent{
		TokenID: event.Topics[3],
		Owner:   event.Topics[2],
	}, nil
}
