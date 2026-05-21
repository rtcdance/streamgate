package event

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"go.uber.org/zap"
)

type ParsedEvent struct {
	Name      string                 `json:"name"`
	Address   string                 `json:"address"`
	Signature string                 `json:"signature"`
	Args      map[string]interface{} `json:"args"`
}

type EventParser struct {
	logger *zap.Logger
	abis   map[string]abi.ABI
}

func NewEventParser(logger *zap.Logger) *EventParser {
	ep := &EventParser{
		logger: logger,
		abis:   make(map[string]abi.ABI),
	}

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

func (ep *EventParser) parseLog(log *types.Log, sig string) ParsedEvent {
	for name, parsedABI := range ep.abis {
		for _, event := range parsedABI.Events {
			if event.ID.Hex() != sig {
				continue
			}

			indexedCount := 0
			for _, input := range event.Inputs {
				if input.Indexed {
					indexedCount++
				}
			}
			expectedTopics := indexedCount + 1
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

func unpackEventArgs(parsedABI abi.ABI, eventName string, log *types.Log, out map[string]interface{}) error {
	event, ok := parsedABI.Events[eventName]
	if !ok {
		return fmt.Errorf("event %s not found in ABI", eventName)
	}

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

	indexed := 0
	for _, input := range event.Inputs {
		if input.Indexed {
			topicIdx := indexed + 1
			if topicIdx < len(log.Topics) {
				out[input.Name] = formatIndexedValue(log.Topics[topicIdx], input.Type.String())
			}
			indexed++
		}
	}

	return nil
}

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

func formatIndexedValue(topic common.Hash, typ string) string {
	if typ == "address" {
		return common.BytesToAddress(topic[12:]).Hex()
	}
	return topic.Hex()
}

const ERC20EventABI = `[
  {"anonymous":false,"inputs":[{"indexed":true,"name":"from","type":"address"},{"indexed":true,"name":"to","type":"address"},{"indexed":false,"name":"value","type":"uint256"}],"name":"Transfer","type":"event"},
  {"anonymous":false,"inputs":[{"indexed":true,"name":"owner","type":"address"},{"indexed":true,"name":"spender","type":"address"},{"indexed":false,"name":"value","type":"uint256"}],"name":"Approval","type":"event"}
]`

const NFTTransferABI = `[
  {"anonymous":false,"inputs":[{"indexed":true,"name":"from","type":"address"},{"indexed":true,"name":"to","type":"address"},{"indexed":true,"name":"tokenId","type":"uint256"}],"name":"Transfer","type":"event"},
  {"anonymous":false,"inputs":[{"indexed":true,"name":"owner","type":"address"},{"indexed":true,"name":"approved","type":"address"},{"indexed":true,"name":"tokenId","type":"uint256"}],"name":"Approval","type":"event"},
  {"anonymous":false,"inputs":[{"indexed":true,"name":"owner","type":"address"},{"indexed":true,"name":"operator","type":"address"},{"indexed":false,"name":"approved","type":"bool"}],"name":"ApprovalForAll","type":"event"}
]`

const ERC1155EventABI = `[
  {"anonymous":false,"inputs":[{"indexed":true,"name":"operator","type":"address"},{"indexed":true,"name":"from","type":"address"},{"indexed":true,"name":"to","type":"address"},{"indexed":false,"name":"id","type":"uint256"},{"indexed":false,"name":"value","type":"uint256"}],"name":"TransferSingle","type":"event"},
  {"anonymous":false,"inputs":[{"indexed":true,"name":"operator","type":"address"},{"indexed":true,"name":"from","type":"address"},{"indexed":true,"name":"to","type":"address"},{"indexed":false,"name":"ids","type":"uint256[]"},{"indexed":false,"name":"values","type":"uint256[]"}],"name":"TransferBatch","type":"event"},
  {"anonymous":false,"inputs":[{"indexed":true,"name":"account","type":"address"},{"indexed":true,"name":"operator","type":"address"},{"indexed":false,"name":"approved","type":"bool"}],"name":"ApprovalForAll","type":"event"}
]`
