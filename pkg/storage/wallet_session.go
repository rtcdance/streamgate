package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/go-redis/redis/v8"
)

var (
	ErrWalletSessionMissing     = errors.New("wallet session proof missing or invalid")
	ErrWalletSessionUnavailable = errors.New("wallet session proof storage unavailable")
	ErrWalletSessionConflict    = errors.New("wallet session changed or revoked")
)

// WalletSession is a server-only record. Never return its original message or
// signature in an API response or log: refresh reuses this admitted proof.
type WalletSession struct {
	Schema            int       `json:"schema"`
	SessionID         string    `json:"session_id"`
	ProofID           string    `json:"proof_id"`
	Version           uint64    `json:"version"`
	Wallet            string    `json:"wallet"`
	ChainID           int64     `json:"chain_id"`
	Kind              string    `json:"kind"`
	AuthEpoch         int64     `json:"auth_epoch"`
	RestoreEpoch      string    `json:"restore_epoch"`
	CurrentJTI        string    `json:"current_jti"`
	Scheme            string    `json:"scheme"`
	Message           string    `json:"message"`
	Signature         string    `json:"signature"`
	Digest            string    `json:"digest"`
	CodeHash          string    `json:"code_hash"`
	BlockNumber       uint64    `json:"block_number"`
	BlockHash         string    `json:"block_hash"`
	ObservedAt        time.Time `json:"observed_at"`
	AdmittedAt        time.Time `json:"admitted_at"`
	AbsoluteExpiresAt time.Time `json:"absolute_expires_at"`
	LeaseExpiresAt    time.Time `json:"lease_expires_at"`
}

// WalletSessionStore atomically binds proof rotation to the authoritative
// session epoch. A standalone process map is never the runtime fallback.
type WalletSessionStore interface {
	Create(context.Context, *WalletSession) error
	Load(context.Context, string) (*WalletSession, error)
	CompareAndSwap(context.Context, *WalletSession, *WalletSession) error
}

type RedisWalletSessionStore struct{ client *redis.Client }

func NewRedisWalletSessionStore(client *redis.Client) (*RedisWalletSessionStore, error) {
	if client == nil {
		return nil, ErrWalletSessionUnavailable
	}
	return &RedisWalletSessionStore{client: client}, nil
}

func walletSessionKey(id string) string { return "streamgate:wallet-proof:v1:" + id }

func validateWalletSession(r *WalletSession) error {
	if r == nil || r.Schema != 1 || r.SessionID == "" || r.ProofID == "" || r.Version == 0 || r.CurrentJTI == "" || r.AuthEpoch <= 0 || r.RestoreEpoch == "" || r.ChainID <= 0 || r.Wallet == "" || r.Kind != "erc1271" || r.Message == "" || r.Signature == "" || r.Digest == "" || r.CodeHash == "" || r.BlockHash == "" {
		return ErrWalletSessionMissing
	}
	if r.Scheme != "siwe" && r.Scheme != "personal_sign" {
		return ErrWalletSessionMissing
	}
	if r.ObservedAt.IsZero() || r.AdmittedAt.IsZero() || !r.AbsoluteExpiresAt.After(r.AdmittedAt) || r.AbsoluteExpiresAt.Sub(r.AdmittedAt) > 2*time.Hour || r.LeaseExpiresAt.After(r.AbsoluteExpiresAt) || !r.LeaseExpiresAt.After(r.ObservedAt) || r.LeaseExpiresAt.Sub(r.ObservedAt) > 65*time.Second {
		return ErrWalletSessionMissing
	}
	return nil
}

// The epoch and proof are checked in ONE Redis operation. Neither an expired
// session nor a late successful verification may recreate/reset that epoch.
var createWalletSession = redis.NewScript(`
if redis.call('GET', KEYS[1]) ~= ARGV[1] or redis.call('EXISTS', KEYS[2]) ~= 0 then return 0 end
local clock = redis.call('TIME')
if tonumber(ARGV[3]) <= tonumber(clock[1])*1000 + math.floor(tonumber(clock[2])/1000) then return 0 end
redis.call('SET', KEYS[2], ARGV[2])
redis.call('PEXPIREAT', KEYS[2], ARGV[3])
return 1`)

var rotateWalletSession = redis.NewScript(`
if redis.call('GET', KEYS[1]) ~= ARGV[1] or redis.call('GET', KEYS[2]) ~= ARGV[2] then return 0 end
local clock = redis.call('TIME')
if tonumber(ARGV[4]) <= tonumber(clock[1])*1000 + math.floor(tonumber(clock[2])/1000) then return 0 end
redis.call('SET', KEYS[2], ARGV[3])
redis.call('PEXPIREAT', KEYS[2], ARGV[4])
return 1`)

func (s *RedisWalletSessionStore) Create(ctx context.Context, r *WalletSession) error {
	if err := validateWalletSession(r); err != nil {
		return err
	}
	if r.Version != 1 || r.AuthEpoch != 1 {
		return ErrWalletSessionMissing
	}
	data, err := json.Marshal(r)
	if err != nil {
		return ErrWalletSessionMissing
	}
	result, err := createWalletSession.Run(ctx, s.client, []string{sessionStorageKey(r.SessionID), walletSessionKey(r.SessionID)}, fmt.Sprint(r.AuthEpoch), string(data), r.AbsoluteExpiresAt.UnixMilli()).Int()
	if err != nil {
		return ErrWalletSessionUnavailable
	}
	if result != 1 {
		return ErrWalletSessionConflict
	}
	return nil
}

func (s *RedisWalletSessionStore) Load(ctx context.Context, id string) (*WalletSession, error) {
	if id == "" {
		return nil, ErrWalletSessionMissing
	}
	raw, err := s.client.Get(ctx, walletSessionKey(id)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, ErrWalletSessionMissing
	}
	if err != nil {
		return nil, ErrWalletSessionUnavailable
	}
	var r WalletSession
	if json.Unmarshal(raw, &r) != nil || validateWalletSession(&r) != nil || r.SessionID != id {
		return nil, ErrWalletSessionMissing
	}
	return &r, nil
}

func (s *RedisWalletSessionStore) CompareAndSwap(ctx context.Context, old, next *WalletSession) error {
	if validateWalletSession(old) != nil || validateWalletSession(next) != nil {
		return ErrWalletSessionMissing
	}
	// A renewal may change observation/lease/JTI, never the admitted identity,
	// original proof or absolute lifetime. Full-record CAS also binds version/JTI.
	if old.SessionID != next.SessionID || old.ProofID != next.ProofID || old.Wallet != next.Wallet || old.ChainID != next.ChainID || old.Kind != next.Kind || old.AuthEpoch != next.AuthEpoch || old.RestoreEpoch != next.RestoreEpoch || old.Scheme != next.Scheme || old.Message != next.Message || old.Signature != next.Signature || old.Digest != next.Digest || !old.AdmittedAt.Equal(next.AdmittedAt) || !old.AbsoluteExpiresAt.Equal(next.AbsoluteExpiresAt) || next.Version != old.Version+1 || next.CurrentJTI == old.CurrentJTI || next.ObservedAt.Before(old.ObservedAt) {
		return ErrWalletSessionConflict
	}
	before, err := json.Marshal(old)
	if err != nil {
		return ErrWalletSessionMissing
	}
	after, err := json.Marshal(next)
	if err != nil {
		return ErrWalletSessionMissing
	}
	result, err := rotateWalletSession.Run(ctx, s.client, []string{sessionStorageKey(old.SessionID), walletSessionKey(old.SessionID)}, fmt.Sprint(old.AuthEpoch), string(before), string(after), next.AbsoluteExpiresAt.UnixMilli()).Int()
	if err != nil {
		return ErrWalletSessionUnavailable
	}
	if result != 1 {
		return ErrWalletSessionConflict
	}
	return nil
}
