package web3

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"go.uber.org/zap"
)

// ParsedEvent represents a decoded event log from a transaction receipt.
type ParsedEvent struct {
	Name      string                 `json:"name"`
	Address   string                 `json:"address"`   // contract that emitted the event
	Signature string                 `json:"signature"` // keccak256 topic[0]
	Args      map[string]interface{} `json:"args"`      // decoded parameters
}

// EventParser decodes common ERC-20/ERC-721/ERC-1155 events from receipt logs.
type EventParser struct {
	logger *zap.Logger
	abis   map[string]abi.ABI // event signature → ABI (for lookup)
}

// NewEventParser creates an EventParser that knows the standard
// Transfer, Approval, and ApprovalForAll events.
func NewEventParser(logger *zap.Logger) *EventParser {
	ep := &EventParser{
		logger: logger,
		abis:   make(map[string]abi.ABI),
	}

	// Parse the standard event ABIs
	for name, jsonStr := range map[string]string{
		"erc20":   ERC20EventABI,
		"erc721":  NFTTransferABI,
		"erc1155": ERC1155EventABI,
	} {
		parsed, err := abi.JSON(strings.NewReader(jsonStr))
		if err != nil {
			logger.Warn("EventParser: failed to parse ABI, skipping", zap.String("name", name), zap.Error(err))
			continue
		}
		ep.abis[name] = parsed
	}

	return ep
}

// ParseLogs decodes all recognizable events from the given receipt logs.
// Unrecognized logs are returned as ParsedEvent with Name="Unknown" and
// Args containing the raw topics.
func (ep *EventParser) ParseLogs(logs []*types.Log) []ParsedEvent {
	if len(logs) == 0 {
		return nil
	}

	var events []ParsedEvent
	for _, log := range logs {
		if len(log.Topics) == 0 {
			continue
		}
		sig := log.Topics[0].Hex()
		evt := ep.parseLog(log, sig)
		events = append(events, evt)
	}
	return events
}

// parseLog attempts to decode a single log entry against known ABIs.
func (ep *EventParser) parseLog(log *types.Log, sig string) ParsedEvent {
	// Try each ABI for a matching event
	for name, parsedABI := range ep.abis {
		for _, event := range parsedABI.Events {
			if event.ID.Hex() != sig {
				continue
			}

			// Disambiguate ERC-20 vs ERC-721 Transfer/Approval by topic count.
			// ERC-20: 2 indexed args (from, to) → 3 topics total (sig + 2)
			// ERC-721: 3 indexed args (from, to, tokenId) → 4 topics total (sig + 3)
			indexedCount := 0
			for _, input := range event.Inputs {
				if input.Indexed {
					indexedCount++
				}
			}
			expectedTopics := indexedCount + 1 // +1 for signature topic
			if len(log.Topics) != expectedTopics {
				continue
			}

			args := make(map[string]interface{})
			if err := unpackEventArgs(parsedABI, event.Name, log, args); err != nil {
				ep.logger.Debug("EventParser: failed to unpack event args",
					zap.String("event", event.Name),
					zap.String("abi_source", name),
					zap.Error(err))
				continue
			}

			return ParsedEvent{
				Name:      event.Name,
				Address:   log.Address.Hex(),
				Signature: sig,
				Args:      args,
			}
		}
	}

	// Unknown event — return raw topics
	rawTopics := make([]string, len(log.Topics))
	for i, t := range log.Topics {
		rawTopics[i] = t.Hex()
	}
	return ParsedEvent{
		Name:      "Unknown",
		Address:   log.Address.Hex(),
		Signature: sig,
		Args:      map[string]interface{}{"raw_topics": rawTopics, "data": fmt.Sprintf("0x%x", log.Data)},
	}
}

// unpackEventArgs decodes indexed and non-indexed event arguments.
func unpackEventArgs(parsedABI abi.ABI, eventName string, log *types.Log, out map[string]interface{}) error {
	event, ok := parsedABI.Events[eventName]
	if !ok {
		return fmt.Errorf("event %s not found in ABI", eventName)
	}

	// Decode non-indexed args from Data
	if len(log.Data) > 0 {
		vals, err := event.Inputs.Unpack(log.Data)
		if err != nil {
			return fmt.Errorf("unpack data: %w", err)
		}
		nonIndexed := 0
		for _, input := range event.Inputs {
			if !input.Indexed {
				if nonIndexed < len(vals) {
					out[input.Name] = formatValue(vals[nonIndexed])
					nonIndexed++
				}
			}
		}
	}

	// Decode indexed args from Topics (topic[0] is the event signature)
	indexed := 0
	for _, input := range event.Inputs {
		if input.Indexed {
			topicIdx := indexed + 1 // +1 because topic[0] is the signature
			if topicIdx < len(log.Topics) {
				out[input.Name] = formatIndexedValue(log.Topics[topicIdx], input.Type.String())
			}
			indexed++
		}
	}

	return nil
}

// formatValue formats a decoded non-indexed value for JSON serialization.
func formatValue(v interface{}) interface{} {
	switch val := v.(type) {
	case *big.Int:
		return val.String()
	case common.Address:
		return val.Hex()
	case []common.Address:
		addrs := make([]string, len(val))
		for i, a := range val {
			addrs[i] = a.Hex()
		}
		return addrs
	case string:
		return val
	case bool:
		return val
	case []byte:
		return fmt.Sprintf("0x%x", val)
	default:
		return fmt.Sprintf("%v", val)
	}
}

// formatIndexedValue decodes an indexed topic.
func formatIndexedValue(topic common.Hash, typ string) string {
	// Address types are stored in the last 20 bytes of the topic
	if typ == "address" {
		return common.BytesToAddress(topic[12:]).Hex()
	}
	// For uint256 and other value types, return hex
	return topic.Hex()
}

// ERC20EventABI contains ERC-20 Transfer and Approval events.
const ERC20EventABI = `[
  {"anonymous":false,"inputs":[{"indexed":true,"name":"from","type":"address"},{"indexed":true,"name":"to","type":"address"},{"indexed":false,"name":"value","type":"uint256"}],"name":"Transfer","type":"event"},
  {"anonymous":false,"inputs":[{"indexed":true,"name":"owner","type":"address"},{"indexed":true,"name":"spender","type":"address"},{"indexed":false,"name":"value","type":"uint256"}],"name":"Approval","type":"event"}
]`

// NFTTransferABI contains the minimal ABI for ERC-721 Transfer/Approval events.
const NFTTransferABI = `[
  {"anonymous":false,"inputs":[{"indexed":true,"name":"from","type":"address"},{"indexed":true,"name":"to","type":"address"},{"indexed":true,"name":"tokenId","type":"uint256"}],"name":"Transfer","type":"event"},
  {"anonymous":false,"inputs":[{"indexed":true,"name":"owner","type":"address"},{"indexed":true,"name":"approved","type":"address"},{"indexed":true,"name":"tokenId","type":"uint256"}],"name":"Approval","type":"event"},
  {"anonymous":false,"inputs":[{"indexed":true,"name":"owner","type":"address"},{"indexed":true,"name":"operator","type":"address"},{"indexed":false,"name":"approved","type":"bool"}],"name":"ApprovalForAll","type":"event"}
]`

// ERC1155EventABI contains ERC-1155 TransferSingle, TransferBatch, and ApprovalForAll events.
const ERC1155EventABI = `[
  {"anonymous":false,"inputs":[{"indexed":true,"name":"operator","type":"address"},{"indexed":true,"name":"from","type":"address"},{"indexed":true,"name":"to","type":"address"},{"indexed":false,"name":"id","type":"uint256"},{"indexed":false,"name":"value","type":"uint256"}],"name":"TransferSingle","type":"event"},
  {"anonymous":false,"inputs":[{"indexed":true,"name":"operator","type":"address"},{"indexed":true,"name":"from","type":"address"},{"indexed":true,"name":"to","type":"address"},{"indexed":false,"name":"ids","type":"uint256[]"},{"indexed":false,"name":"values","type":"uint256[]"}],"name":"TransferBatch","type":"event"},
  {"anonymous":false,"inputs":[{"indexed":true,"name":"account","type":"address"},{"indexed":true,"name":"operator","type":"address"},{"indexed":false,"name":"approved","type":"bool"}],"name":"ApprovalForAll","type":"event"}
]`
