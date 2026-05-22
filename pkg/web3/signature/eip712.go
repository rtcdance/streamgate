package signature

import (
	"crypto/ecdsa"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/signer/core/apitypes"
	"go.uber.org/zap"
)

type EIP712TypedData struct {
	Types       EIP712Types               `json:"types"`
	PrimaryType string                    `json:"primaryType"`
	Domain      EIP712Domain              `json:"domain"`
	Message     apitypes.TypedDataMessage `json:"message"`
}

type EIP712Types map[string][]EIP712Type

type EIP712Type struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type EIP712Domain struct {
	Name              string   `json:"name"`
	Version           string   `json:"version"`
	ChainId           *big.Int `json:"chainId"`
	VerifyingContract string   `json:"verifyingContract"`
	Salt              string   `json:"salt"`
}

type EIP712Verifier struct {
	logger *zap.Logger
}

func NewEIP712Verifier(logger *zap.Logger) *EIP712Verifier {
	return &EIP712Verifier{
		logger: logger,
	}
}

func (ev *EIP712Verifier) VerifyTypedData(address string, typedData *EIP712TypedData, signature string) (bool, error) {
	ev.logger.Debug("Verifying EIP-712 typed data",
		zap.String("address", address),
		zap.String("primary_type", typedData.PrimaryType))

	if !strings.HasPrefix(address, "0x") {
		address = "0x" + address
	}

	if !strings.HasPrefix(signature, "0x") {
		signature = "0x" + signature
	}

	sig := common.FromHex(signature)
	if len(sig) != 65 {
		return false, fmt.Errorf("invalid signature length: expected 65, got %d", len(sig))
	}

	if sig[64] >= 27 {
		sig[64] -= 27
	}

	apiTypedData := ev.convertToAPITypes(typedData)

	hash, err := hashTypedData(apiTypedData)
	if err != nil {
		return false, fmt.Errorf("failed to hash typed data: %w", err)
	}

	pubKey, err := crypto.SigToPub(hash, sig)
	if err != nil {
		return false, fmt.Errorf("failed to recover public key: %w", err)
	}

	recoveredAddress := crypto.PubkeyToAddress(*pubKey)

	expectedAddress := common.HexToAddress(address)
	if subtle.ConstantTimeCompare(recoveredAddress.Bytes(), expectedAddress.Bytes()) != 1 {
		ev.logger.Warn("EIP-712 signature verification failed",
			zap.String("expected", expectedAddress.Hex()),
			zap.String("recovered", recoveredAddress.Hex()))
		return false, nil
	}

	ev.logger.Debug("EIP-712 signature verified successfully", zap.String("address", address))
	return true, nil
}

func (ev *EIP712Verifier) SignTypedData(typedData *EIP712TypedData, privateKey *ecdsa.PrivateKey) (string, error) {
	ev.logger.Debug("Signing EIP-712 typed data",
		zap.String("primary_type", typedData.PrimaryType))

	apiTypedData := ev.convertToAPITypes(typedData)

	hash, err := hashTypedData(apiTypedData)
	if err != nil {
		return "", fmt.Errorf("failed to hash typed data: %w", err)
	}

	sig, err := crypto.Sign(hash, privateKey)
	if err != nil {
		return "", fmt.Errorf("failed to sign typed data: %w", err)
	}

	if sig[64] < 27 {
		sig[64] += 27
	}

	return "0x" + common.Bytes2Hex(sig), nil
}

func (ev *EIP712Verifier) convertToAPITypes(typedData *EIP712TypedData) apitypes.TypedData {
	apiTypes := make(map[string][]apitypes.Type)
	for typeName, types := range typedData.Types {
		apiTypes[typeName] = make([]apitypes.Type, len(types))
		for i, t := range types {
			apiTypes[typeName][i] = apitypes.Type{
				Name: t.Name,
				Type: t.Type,
			}
		}
	}

	return apitypes.TypedData{
		Types:       apiTypes,
		PrimaryType: typedData.PrimaryType,
		Domain: apitypes.TypedDataDomain{
			Name:              typedData.Domain.Name,
			Version:           typedData.Domain.Version,
			ChainId:           (*math.HexOrDecimal256)(typedData.Domain.ChainId),
			VerifyingContract: typedData.Domain.VerifyingContract,
			Salt:              typedData.Domain.Salt,
		},
		Message: typedData.Message,
	}
}

func hashTypedData(typedData apitypes.TypedData) ([]byte, error) {
	domainSeparator, err := typedData.HashStruct("EIP712Domain", typedData.Domain.Map())
	if err != nil {
		return nil, err
	}

	messageHash, err := typedData.HashStruct(typedData.PrimaryType, typedData.Message)
	if err != nil {
		return nil, err
	}

	rawData := append([]byte("\x19\x01"), domainSeparator...)
	rawData = append(rawData, messageHash...)

	return crypto.Keccak256(rawData), nil
}

func CreatePermitTypedData(domain EIP712Domain, owner, spender string, value, nonce, deadline *big.Int) *EIP712TypedData {
	return &EIP712TypedData{
		Types: EIP712Types{
			"EIP712Domain": []EIP712Type{
				{Name: "name", Type: "string"},
				{Name: "version", Type: "string"},
				{Name: "chainId", Type: "uint256"},
				{Name: "verifyingContract", Type: "address"},
			},
			"Permit": []EIP712Type{
				{Name: "owner", Type: "address"},
				{Name: "spender", Type: "address"},
				{Name: "value", Type: "uint256"},
				{Name: "nonce", Type: "uint256"},
				{Name: "deadline", Type: "uint256"},
			},
		},
		PrimaryType: "Permit",
		Domain:      domain,
		Message: apitypes.TypedDataMessage{
			"owner":    owner,
			"spender":  spender,
			"value":    value.String(),
			"nonce":    nonce.String(),
			"deadline": deadline.String(),
		},
	}
}

func CreateDelegationTypedData(domain EIP712Domain, delegatee, authority string, chainId *big.Int, nonce uint64) *EIP712TypedData {
	return &EIP712TypedData{
		Types: EIP712Types{
			"EIP712Domain": []EIP712Type{
				{Name: "name", Type: "string"},
				{Name: "version", Type: "string"},
				{Name: "chainId", Type: "uint256"},
				{Name: "verifyingContract", Type: "address"},
			},
			"Delegation": []EIP712Type{
				{Name: "delegatee", Type: "address"},
				{Name: "authority", Type: "address"},
				{Name: "chainId", Type: "uint256"},
				{Name: "nonce", Type: "uint256"},
			},
		},
		PrimaryType: "Delegation",
		Domain:      domain,
		Message: apitypes.TypedDataMessage{
			"delegatee": delegatee,
			"authority": authority,
			"chainId":   chainId.String(),
			"nonce":     fmt.Sprintf("%d", nonce),
		},
	}
}

func ParseTypedDataFromJSON(jsonData []byte) (*EIP712TypedData, error) {
	var typedData EIP712TypedData
	if err := json.Unmarshal(jsonData, &typedData); err != nil {
		return nil, fmt.Errorf("failed to parse typed data JSON: %w", err)
	}
	return &typedData, nil
}

func EncodeType(typeName string, types []EIP712Type) string {
	var sb strings.Builder
	sb.WriteString(typeName)
	sb.WriteString("(")

	for i, t := range types {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(t.Type)
		sb.WriteString(" ")
		sb.WriteString(t.Name)
	}

	sb.WriteString(")")
	return sb.String()
}

func TypeHash(typeName string, types []EIP712Type) []byte {
	encodedType := EncodeType(typeName, types)
	return crypto.Keccak256([]byte(encodedType))
}

func EncodeData(primaryType string, types EIP712Types, data map[string]interface{}) ([]byte, error) {
	var encodedTypes []string

	encodedTypes = append(encodedTypes, EncodeType(primaryType, types[primaryType]))

	encodedTypes = append(encodedTypes, findDependencies(primaryType, types)...)

	joined := strings.Join(encodedTypes, "")
	return crypto.Keccak256([]byte(joined)), nil
}

func findDependencies(typeName string, types EIP712Types) []string {
	visited := make(map[string]bool)
	return findDependenciesHelper(typeName, types, visited)
}

func findDependenciesHelper(typeName string, types EIP712Types, visited map[string]bool) []string {
	if visited[typeName] {
		return nil
	}

	visited[typeName] = true
	var dependencies []string

	for _, t := range types[typeName] {
		if isReferenceType(t.Type) {
			dependencies = append(dependencies, findDependenciesHelper(t.Type, types, visited)...)
		}
	}

	dependencies = append(dependencies, EncodeType(typeName, types[typeName]))
	return dependencies
}

func isReferenceType(typeName string) bool {
	primitives := map[string]bool{
		"address": true,
		"bool":    true,
		"string":  true,
		"bytes":   true,
	}

	if primitives[typeName] {
		return false
	}

	if strings.HasPrefix(typeName, "bytes") {
		return false
	}

	if strings.HasPrefix(typeName, "uint") || strings.HasPrefix(typeName, "int") {
		return false
	}

	return true
}

func HashStruct(primaryType string, types EIP712Types, data map[string]interface{}) ([]byte, error) {
	typeHash := TypeHash(primaryType, types[primaryType])

	var dataParts [][]byte
	for _, t := range types[primaryType] {
		value, ok := data[t.Name]
		if !ok {
			return nil, fmt.Errorf("missing value for field: %s", t.Name)
		}

		hashed, err := hashValue(t.Type, types, value)
		if err != nil {
			return nil, fmt.Errorf("failed to hash field %s: %w", t.Name, err)
		}

		dataParts = append(dataParts, hashed)
	}

	var result []byte
	result = append(result, typeHash...)
	for _, part := range dataParts {
		result = append(result, part...)
	}

	return crypto.Keccak256(result), nil
}

func hashValue(typeName string, types EIP712Types, value interface{}) ([]byte, error) {
	switch typeName {
	case "address":
		addr, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("expected string for address, got %T", value)
		}
		if !strings.HasPrefix(addr, "0x") {
			addr = "0x" + addr
		}
		return common.FromHex(addr), nil

	case "bool":
		b, ok := value.(bool)
		if !ok {
			return nil, fmt.Errorf("expected bool, got %T", value)
		}
		result := make([]byte, 32)
		if b {
			result[31] = 1
		}
		return result, nil

	case "string":
		s, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("expected string, got %T", value)
		}
		return crypto.Keccak256([]byte(s)), nil

	case "bytes":
		b, ok := value.([]byte)
		if !ok {
			s, ok := value.(string)
			if !ok {
				return nil, fmt.Errorf("expected []byte or string for bytes, got %T", value)
			}
			b = common.FromHex(s)
		}
		return crypto.Keccak256(b), nil
	}

	if strings.HasPrefix(typeName, "bytes") {
		b, ok := value.([]byte)
		if !ok {
			s, ok := value.(string)
			if !ok {
				return nil, fmt.Errorf("expected []byte or string for %s, got %T", typeName, value)
			}
			b = common.FromHex(s)
		}

		size, err := parseSize(typeName)
		if err != nil {
			return nil, err
		}

		if len(b) > size {
			b = b[:size]
		}

		result := make([]byte, 32)
		copy(result, b)
		return result, nil
	}

	if strings.HasPrefix(typeName, "uint") || strings.HasPrefix(typeName, "int") {
		var bigInt *big.Int
		switch v := value.(type) {
		case *big.Int:
			bigInt = v
		case string:
			bigInt = new(big.Int)
			bigInt.SetString(v, 10)
		case float64:
			bigInt = new(big.Int).SetInt64(int64(v))
		case int:
			bigInt = new(big.Int).SetInt64(int64(v))
		case int64:
			bigInt = new(big.Int).SetInt64(v)
		default:
			return nil, fmt.Errorf("unsupported value type for %s: %T", typeName, value)
		}

		result := make([]byte, 32)
		bigInt.FillBytes(result)
		return result, nil
	}

	if isReferenceType(typeName) {
		dataMap, ok := value.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("expected map for reference type %s, got %T", typeName, value)
		}
		return HashStruct(typeName, types, dataMap)
	}

	return nil, fmt.Errorf("unsupported type: %s", typeName)
}

func parseSize(typeName string) (int, error) {
	parts := strings.Split(typeName, "uint")
	if len(parts) == 2 {
		size, err := strconv.Atoi(parts[1])
		if err != nil {
			return 0, err
		}
		if size <= 0 || size > 256 || size%8 != 0 {
			return 0, fmt.Errorf("invalid uint size: %d", size)
		}
		return size, nil
	}

	parts = strings.Split(typeName, "int")
	if len(parts) == 2 {
		size, err := strconv.Atoi(parts[1])
		if err != nil {
			return 0, err
		}
		if size <= 0 || size > 256 || size%8 != 0 {
			return 0, fmt.Errorf("invalid int size: %d", size)
		}
		return size, nil
	}

	parts = strings.Split(typeName, "bytes")
	if len(parts) == 2 {
		size, err := strconv.Atoi(parts[1])
		if err != nil {
			return 0, err
		}
		if size <= 0 || size > 32 {
			return 0, fmt.Errorf("invalid bytes size: %d", size)
		}
		return size, nil
	}

	return 0, fmt.Errorf("cannot parse size from type: %s", typeName)
}
