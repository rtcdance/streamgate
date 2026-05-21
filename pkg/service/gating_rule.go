package service

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/rtcdance/streamgate/pkg/models"
	"github.com/rtcdance/streamgate/pkg/storage"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

type GatingRuleService struct {
	db     storage.DB
	logger *zap.Logger
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
