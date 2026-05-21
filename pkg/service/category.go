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

type CategoryService struct {
	db     storage.DB
	logger *zap.Logger
}

func NewCategoryService(db storage.DB, logger *zap.Logger) *CategoryService {
	return &CategoryService{db: db, logger: logger}
}

func (s *CategoryService) CreateCategory(ctx context.Context, cat *models.Category) (string, error) {
	if s.db == nil {
		return "", fmt.Errorf("database not available")
	}
	if cat.ID == "" {
		cat.ID = uuid.New().String()
	}
	cat.CreatedAt = time.Now()

	query := `INSERT INTO content_categories (id, name, slug, description, parent_id, created_at) VALUES ($1, $2, $3, $4, $5, $6)`
	_, err := s.db.Exec(ctx, query, cat.ID, cat.Name, cat.Slug, cat.Description, nilIfEmpty(cat.ParentID), cat.CreatedAt)
	if err != nil {
		return "", fmt.Errorf("failed to create category: %w", err)
	}
	return cat.ID, nil
}

func (s *CategoryService) GetCategory(ctx context.Context, id string) (*models.Category, error) {
	if s.db == nil {
		return nil, fmt.Errorf("database not available")
	}
	query := `SELECT id, name, slug, description, parent_id, created_at FROM content_categories WHERE id = $1`
	var cat models.Category
	var parentID sql.NullString
	err := s.db.QueryRow(ctx, query, id).Scan(&cat.ID, &cat.Name, &cat.Slug, &cat.Description, &parentID, &cat.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("category not found %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query category: %w", err)
	}
	cat.ParentID = parentID.String
	return &cat, nil
}

func (s *CategoryService) ListCategories(ctx context.Context) ([]*models.Category, error) {
	if s.db == nil {
		return nil, fmt.Errorf("database not available")
	}
	query := `SELECT id, name, slug, description, parent_id, created_at FROM content_categories ORDER BY name ASC`
	rows, err := s.db.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list categories: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var cats []*models.Category
	for rows.Next() {
		var cat models.Category
		var parentID sql.NullString
		if err := rows.Scan(&cat.ID, &cat.Name, &cat.Slug, &cat.Description, &parentID, &cat.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan category: %w", err)
		}
		cat.ParentID = parentID.String
		cats = append(cats, &cat)
	}
	return cats, nil
}

func (s *CategoryService) UpdateCategory(ctx context.Context, cat *models.Category) error {
	if s.db == nil {
		return fmt.Errorf("database not available")
	}
	query := `UPDATE content_categories SET name = $2, slug = $3, description = $4, parent_id = $5 WHERE id = $1`
	result, err := s.db.Exec(ctx, query, cat.ID, cat.Name, cat.Slug, cat.Description, nilIfEmpty(cat.ParentID))
	if err != nil {
		return fmt.Errorf("failed to update category: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("category not found %s: %w", cat.ID, ErrNotFound)
	}
	return nil
}

func (s *CategoryService) DeleteCategory(ctx context.Context, id string) error {
	if s.db == nil {
		return fmt.Errorf("database not available")
	}
	result, err := s.db.Exec(ctx, "DELETE FROM content_categories WHERE id = $1", id)
	if err != nil {
		return fmt.Errorf("failed to delete category: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("category not found %s: %w", id, ErrNotFound)
	}
	return nil
}

func (s *CategoryService) BindContent(ctx context.Context, contentID, categoryID string) error {
	if s.db == nil {
		return fmt.Errorf("database not available")
	}
	_, err := s.db.Exec(ctx, `INSERT INTO content_category_bindings (content_id, category_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, contentID, categoryID)
	if err != nil {
		return fmt.Errorf("failed to bind content to category: %w", err)
	}
	return nil
}

func (s *CategoryService) UnbindContent(ctx context.Context, contentID, categoryID string) error {
	if s.db == nil {
		return fmt.Errorf("database not available")
	}
	_, err := s.db.Exec(ctx, `DELETE FROM content_category_bindings WHERE content_id = $1 AND category_id = $2`, contentID, categoryID)
	if err != nil {
		return fmt.Errorf("failed to unbind content from category: %w", err)
	}
	return nil
}

func (s *CategoryService) ListContentByCategory(ctx context.Context, categoryID string, limit, offset int) ([]string, error) {
	if s.db == nil {
		return nil, fmt.Errorf("database not available")
	}
	query := `SELECT content_id FROM content_category_bindings WHERE category_id = $1 ORDER BY content_id LIMIT $2 OFFSET $3`
	rows, err := s.db.Query(ctx, query, categoryID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to list content by category: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("failed to scan content id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func nilIfEmpty(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}
