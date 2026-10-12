package gating

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/rtcdance/streamgate/pkg/models"
	"github.com/rtcdance/streamgate/pkg/storage"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// Ownership failures are distinguished so the HTTP layer can answer a stable
// 404 (the resource is not there) or 403 (it is there, but not yours) without
// leaking which of the two it was for someone else's content.
var (
	// ErrRuleNotFound means no rule with that id exists.
	ErrRuleNotFound = errors.New("gating rule not found")
	// ErrContentNotFound means the content a rule would attach to does not exist.
	ErrContentNotFound = errors.New("content not found")
	// ErrRuleForbidden means the actor does not own the content.
	ErrRuleForbidden = errors.New("actor does not own this content")
)

type GatingRuleService struct {
	db     storage.DB
	logger *zap.Logger

	invalidatorMu sync.RWMutex
	// invalidator is notified after a rule write so caches holding the previous
	// rule set cannot keep authorising against it.
	invalidator func(contentID string)
}

// SetInvalidator registers a callback invoked after every successful rule write.
// The callback receives the affected contentID, or "" when the whole rule set
// should be dropped (a delete no longer knows which content it belonged to).
func (s *GatingRuleService) SetInvalidator(fn func(contentID string)) {
	s.invalidatorMu.Lock()
	defer s.invalidatorMu.Unlock()
	s.invalidator = fn
}

func (s *GatingRuleService) notifyInvalidate(contentID string) {
	s.invalidatorMu.RLock()
	fn := s.invalidator
	s.invalidatorMu.RUnlock()
	if fn != nil {
		fn(contentID)
	}
}

func NewGatingRuleService(db storage.DB, logger *zap.Logger) *GatingRuleService {
	return &GatingRuleService{db: db, logger: logger}
}

func (s *GatingRuleService) CreateRule(ctx context.Context, rule *models.GatingRule) (string, error) {
	if s.db == nil {
		return "", fmt.Errorf("database not available")
	}
	if rule.ID == "" {
		rule.ID = uuid.New().String()
	}
	now := time.Now()
	rule.CreatedAt = now
	rule.UpdatedAt = now
	if rule.Standard == "" {
		rule.Standard = "erc721"
	}
	if rule.MinBalance <= 0 {
		rule.MinBalance = 1
	}

	query := `
		INSERT INTO content_gating_rules (id, content_id, contract_address, token_id, chain_id, standard, min_balance, is_active, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`
	_, err := s.db.Exec(ctx, query,
		rule.ID, rule.ContentID, rule.ContractAddress, rule.TokenID,
		rule.ChainID, rule.Standard, rule.MinBalance, rule.IsActive,
		rule.CreatedAt, rule.UpdatedAt,
	)
	if err != nil {
		return "", fmt.Errorf("failed to create gating rule: %w", err)
	}
	return rule.ID, nil
}

func (s *GatingRuleService) GetRule(ctx context.Context, id string) (*models.GatingRule, error) {
	if s.db == nil {
		return nil, fmt.Errorf("database not available")
	}
	query := `
		SELECT id, content_id, contract_address, token_id, chain_id, standard, min_balance, is_active, created_at, updated_at
		FROM content_gating_rules WHERE id = $1
	`
	var rule models.GatingRule
	err := s.db.QueryRow(ctx, query, id).Scan(
		&rule.ID, &rule.ContentID, &rule.ContractAddress, &rule.TokenID,
		&rule.ChainID, &rule.Standard, &rule.MinBalance, &rule.IsActive,
		&rule.CreatedAt, &rule.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("gating rule not found: %s", id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query gating rule: %w", err)
	}
	return &rule, nil
}

func (s *GatingRuleService) ListRulesByContent(ctx context.Context, contentID string) ([]*models.GatingRule, error) {
	if s.db == nil {
		return nil, fmt.Errorf("database not available")
	}
	query := `
		SELECT id, content_id, contract_address, token_id, chain_id, standard, min_balance, is_active, created_at, updated_at
		FROM content_gating_rules WHERE content_id = $1 ORDER BY created_at ASC
	`
	rows, err := s.db.Query(ctx, query, contentID)
	if err != nil {
		return nil, fmt.Errorf("failed to list gating rules: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var rules []*models.GatingRule
	for rows.Next() {
		var rule models.GatingRule
		if err := rows.Scan(
			&rule.ID, &rule.ContentID, &rule.ContractAddress, &rule.TokenID,
			&rule.ChainID, &rule.Standard, &rule.MinBalance, &rule.IsActive,
			&rule.CreatedAt, &rule.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan gating rule: %w", err)
		}
		rules = append(rules, &rule)
	}
	return rules, nil
}

func (s *GatingRuleService) GetActiveRulesForContent(ctx context.Context, contentID string) ([]*models.GatingRule, error) {
	if s.db == nil {
		return nil, fmt.Errorf("database not available")
	}
	query := `
		SELECT id, content_id, contract_address, token_id, chain_id, standard, min_balance, is_active, created_at, updated_at
		FROM content_gating_rules WHERE content_id = $1 AND is_active = TRUE ORDER BY created_at ASC
	`
	rows, err := s.db.Query(ctx, query, contentID)
	if err != nil {
		return nil, fmt.Errorf("failed to query active gating rules: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var rules []*models.GatingRule
	for rows.Next() {
		var rule models.GatingRule
		if err := rows.Scan(
			&rule.ID, &rule.ContentID, &rule.ContractAddress, &rule.TokenID,
			&rule.ChainID, &rule.Standard, &rule.MinBalance, &rule.IsActive,
			&rule.CreatedAt, &rule.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan gating rule: %w", err)
		}
		rules = append(rules, &rule)
	}
	return rules, nil
}

func (s *GatingRuleService) UpdateRule(ctx context.Context, rule *models.GatingRule) error {
	if s.db == nil {
		return fmt.Errorf("database not available")
	}
	rule.UpdatedAt = time.Now()
	query := `
		UPDATE content_gating_rules
		SET contract_address = $2, token_id = $3, chain_id = $4, standard = $5,
		    min_balance = $6, is_active = $7, updated_at = $8
		WHERE id = $1
	`
	result, err := s.db.Exec(ctx, query,
		rule.ID, rule.ContractAddress, rule.TokenID, rule.ChainID,
		rule.Standard, rule.MinBalance, rule.IsActive, rule.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to update gating rule: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("gating rule not found: %s", rule.ID)
	}
	return nil
}

func (s *GatingRuleService) DeleteRule(ctx context.Context, id string) error {
	if s.db == nil {
		return fmt.Errorf("database not available")
	}
	result, err := s.db.Exec(ctx, "DELETE FROM content_gating_rules WHERE id = $1", id)
	if err != nil {
		return fmt.Errorf("failed to delete gating rule: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("gating rule not found: %s", id)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Ownership-scoped mutations
//
// A gating rule decides who may watch a piece of content, so writing one is an
// act of that content's owner — not of any authenticated user. Every mutation
// below carries the owner condition INSIDE its single SQL statement, so there
// is no window between "check the owner" and "write the rule" in which the
// content's ownership could change.
// ---------------------------------------------------------------------------

// CreateRuleForOwner creates a rule only when actorWallet owns rule.ContentID.
func (s *GatingRuleService) CreateRuleForOwner(ctx context.Context, rule *models.GatingRule, actorWallet string) (string, error) {
	if s.db == nil {
		return "", fmt.Errorf("database not available")
	}
	if strings.TrimSpace(actorWallet) == "" {
		return "", ErrRuleForbidden
	}
	if rule.ID == "" {
		rule.ID = uuid.New().String()
	}
	now := time.Now()
	rule.CreatedAt = now
	rule.UpdatedAt = now
	if rule.Standard == "" {
		rule.Standard = "erc721"
	}
	if rule.MinBalance <= 0 {
		rule.MinBalance = 1
	}

	// $2 (the inserted content_id, VARCHAR) and $11 (the comparison against
	// contents.id, UUID rendered as text) are kept as separate placeholders:
	// reusing one placeholder for both makes Postgres deduce two different types
	// for it and fail the statement.
	query := `
		INSERT INTO content_gating_rules (id, content_id, contract_address, token_id, chain_id, standard, min_balance, is_active, created_at, updated_at)
		SELECT $1, $2, $3, $4, $5, $6, $7, $8, $9, $10
		WHERE EXISTS (
			SELECT 1 FROM contents c
			WHERE c.id::text = $11 AND LOWER(c.owner_id) = LOWER($12)
		)
	`
	result, err := s.db.Exec(ctx, query,
		rule.ID, rule.ContentID, rule.ContractAddress, rule.TokenID,
		rule.ChainID, rule.Standard, rule.MinBalance, rule.IsActive,
		rule.CreatedAt, rule.UpdatedAt, rule.ContentID, actorWallet,
	)
	if err != nil {
		return "", fmt.Errorf("failed to create gating rule: %w", err)
	}
	if rows, _ := result.RowsAffected(); rows == 0 {
		return "", s.classifyContentDenial(ctx, rule.ContentID)
	}
	s.notifyInvalidate(rule.ContentID)
	return rule.ID, nil
}

// UpdateRuleForOwner updates a rule only when actorWallet owns the content the
// rule belongs to. The rule id is resolved to its content inside the statement,
// so a caller cannot retarget someone else's rule by guessing an id.
func (s *GatingRuleService) UpdateRuleForOwner(ctx context.Context, rule *models.GatingRule, actorWallet string) error {
	if s.db == nil {
		return fmt.Errorf("database not available")
	}
	if strings.TrimSpace(actorWallet) == "" {
		return ErrRuleForbidden
	}
	rule.UpdatedAt = time.Now()
	query := `
		UPDATE content_gating_rules r
		SET contract_address = $2, token_id = $3, chain_id = $4, standard = $5,
		    min_balance = $6, is_active = $7, updated_at = $8
		WHERE r.id = $1
		  AND EXISTS (
			SELECT 1 FROM contents c
			WHERE c.id::text = r.content_id AND LOWER(c.owner_id) = LOWER($9)
		  )
	`
	result, err := s.db.Exec(ctx, query,
		rule.ID, rule.ContractAddress, rule.TokenID, rule.ChainID,
		rule.Standard, rule.MinBalance, rule.IsActive, rule.UpdatedAt, actorWallet,
	)
	if err != nil {
		return fmt.Errorf("failed to update gating rule: %w", err)
	}
	if rows, _ := result.RowsAffected(); rows == 0 {
		return s.classifyRuleDenial(ctx, rule.ID)
	}
	s.notifyInvalidate(rule.ContentID)
	return nil
}

// DeleteRuleForOwner deletes a rule only when actorWallet owns its content.
func (s *GatingRuleService) DeleteRuleForOwner(ctx context.Context, id, actorWallet string) error {
	if s.db == nil {
		return fmt.Errorf("database not available")
	}
	if strings.TrimSpace(actorWallet) == "" {
		return ErrRuleForbidden
	}
	query := `
		DELETE FROM content_gating_rules r
		WHERE r.id = $1
		  AND EXISTS (
			SELECT 1 FROM contents c
			WHERE c.id::text = r.content_id AND LOWER(c.owner_id) = LOWER($2)
		  )
	`
	result, err := s.db.Exec(ctx, query, id, actorWallet)
	if err != nil {
		return fmt.Errorf("failed to delete gating rule: %w", err)
	}
	if rows, _ := result.RowsAffected(); rows == 0 {
		return s.classifyRuleDenial(ctx, id)
	}
	// The row is gone, so the content it belonged to is no longer resolvable
	// here; drop the whole rule cache rather than guess.
	s.notifyInvalidate("")
	return nil
}

// classifyContentDenial explains a zero-row create: the content is missing (404)
// or it belongs to someone else (403). It runs only AFTER the write has already
// been refused, so it cannot widen the race window.
func (s *GatingRuleService) classifyContentDenial(ctx context.Context, contentID string) error {
	var owner string
	err := s.db.QueryRow(ctx, "SELECT owner_id FROM contents WHERE id::text = $1", contentID).Scan(&owner)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return ErrContentNotFound
	case err != nil:
		return fmt.Errorf("failed to resolve content owner: %w", err)
	default:
		return ErrRuleForbidden
	}
}

// classifyRuleDenial explains a zero-row update/delete: the rule is missing
// (404) or it belongs to someone else's content (403).
func (s *GatingRuleService) classifyRuleDenial(ctx context.Context, ruleID string) error {
	var contentID string
	err := s.db.QueryRow(ctx,
		"SELECT content_id FROM content_gating_rules WHERE id = $1", ruleID).Scan(&contentID)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return ErrRuleNotFound
	case err != nil:
		return fmt.Errorf("failed to resolve gating rule: %w", err)
	default:
		return ErrRuleForbidden
	}
}
