package gating_test

import (
	"context"
	"errors"
	"testing"

	"github.com/rtcdance/streamgate/pkg/models"
	"github.com/rtcdance/streamgate/pkg/service/gating"
	"github.com/rtcdance/streamgate/pkg/storage"
	"github.com/rtcdance/streamgate/test/helpers"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// A gating rule decides who may watch a piece of content, so writing one must
// be an act of the content's owner. These are the A/B negatives for that rule:
// two distinct wallets, one of which owns the content.
const (
	ownerA = "0xAaAaAaAaAaAaAaAaAaAaAaAaAaAaAaAaAaAaAaAa"
	ownerB = "0xBbBbBbBbBbBbBbBbBbBbBbBbBbBbBbBbBbBbBbBb"
)

// seedContent inserts a content row owned by ownerWallet. It goes straight to
// SQL on purpose: this test is about gating-rule ownership, and pulling the
// object-storage-backed content service in would only add unrelated setup.
func seedContent(t *testing.T, db *storage.Database, ownerWallet string) string {
	t.Helper()
	id := uuid.New().String()
	_, err := db.Exec(context.Background(),
		`INSERT INTO contents (id, title, description, type, owner_id) VALUES ($1, $2, $3, $4, $5)`,
		id, "ownership test", "", "video", ownerWallet)
	require.NoError(t, err)
	return id
}

func newRuleFixture(t *testing.T) (*storage.Database, *gating.GatingRuleService, string) {
	t.Helper()
	db := helpers.SetupTestDB(t)
	if db == nil {
		return nil, nil, ""
	}
	t.Cleanup(func() { helpers.CleanupTestDB(t, db) })
	return db, gating.NewGatingRuleService(db, zap.NewNop()), seedContent(t, db, ownerA)
}

func ruleFor(contentID string) *models.GatingRule {
	return &models.GatingRule{
		ContentID:       contentID,
		ContractAddress: "0x1111111111111111111111111111111111111111",
		ChainID:         31337,
		Standard:        "erc721",
		MinBalance:      1,
		IsActive:        true,
	}
}

func TestGatingRule_OwnerCanCreate(t *testing.T) {
	_, rules, contentID := newRuleFixture(t)
	if rules == nil {
		return
	}

	id, err := rules.CreateRuleForOwner(context.Background(), ruleFor(contentID), ownerA)
	require.NoError(t, err)
	assert.NotEmpty(t, id)
}

// B must not be able to attach a rule to A's content — that would let B decide
// who may watch A's media.
func TestGatingRule_NonOwnerCannotCreate(t *testing.T) {
	_, rules, contentID := newRuleFixture(t)
	if rules == nil {
		return
	}

	_, err := rules.CreateRuleForOwner(context.Background(), ruleFor(contentID), ownerB)
	require.Error(t, err)
	assert.True(t, errors.Is(err, gating.ErrRuleForbidden), "want ErrRuleForbidden, got %v", err)

	// And nothing was written.
	listed, err := rules.ListRulesByContent(context.Background(), contentID)
	require.NoError(t, err)
	assert.Empty(t, listed)
}

func TestGatingRule_NonOwnerCannotUpdate(t *testing.T) {
	_, rules, contentID := newRuleFixture(t)
	if rules == nil {
		return
	}
	ctx := context.Background()

	id, err := rules.CreateRuleForOwner(ctx, ruleFor(contentID), ownerA)
	require.NoError(t, err)

	// B tries to lower the bar on A's rule.
	victim, err := rules.GetRule(ctx, id)
	require.NoError(t, err)
	victim.MinBalance = 999

	err = rules.UpdateRuleForOwner(ctx, victim, ownerB)
	require.Error(t, err)
	assert.True(t, errors.Is(err, gating.ErrRuleForbidden), "want ErrRuleForbidden, got %v", err)

	// A's rule is untouched.
	after, err := rules.GetRule(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, 1, after.MinBalance, "B's update must not have been applied")

	// The owner can still update it.
	victim.MinBalance = 3
	require.NoError(t, rules.UpdateRuleForOwner(ctx, victim, ownerA))
	after, err = rules.GetRule(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, 3, after.MinBalance)
}

func TestGatingRule_NonOwnerCannotDelete(t *testing.T) {
	_, rules, contentID := newRuleFixture(t)
	if rules == nil {
		return
	}
	ctx := context.Background()

	id, err := rules.CreateRuleForOwner(ctx, ruleFor(contentID), ownerA)
	require.NoError(t, err)

	err = rules.DeleteRuleForOwner(ctx, id, ownerB)
	require.Error(t, err)
	assert.True(t, errors.Is(err, gating.ErrRuleForbidden), "want ErrRuleForbidden, got %v", err)

	_, err = rules.GetRule(ctx, id)
	require.NoError(t, err, "the rule must still exist after B's delete")

	require.NoError(t, rules.DeleteRuleForOwner(ctx, id, ownerA))
	_, err = rules.GetRule(ctx, id)
	require.Error(t, err, "the owner's delete must take effect")
}

// A missing resource is a 404, not a 403: the two must stay distinguishable.
func TestGatingRule_MissingResourcesAreNotFound(t *testing.T) {
	_, rules, _ := newRuleFixture(t)
	if rules == nil {
		return
	}
	ctx := context.Background()

	_, err := rules.CreateRuleForOwner(ctx, ruleFor(uuid.New().String()), ownerA)
	require.Error(t, err)
	assert.True(t, errors.Is(err, gating.ErrContentNotFound), "want ErrContentNotFound, got %v", err)

	missing := ruleFor(uuid.New().String())
	missing.ID = uuid.New().String()
	err = rules.UpdateRuleForOwner(ctx, missing, ownerA)
	require.Error(t, err)
	assert.True(t, errors.Is(err, gating.ErrRuleNotFound), "want ErrRuleNotFound, got %v", err)

	err = rules.DeleteRuleForOwner(ctx, uuid.New().String(), ownerA)
	require.Error(t, err)
	assert.True(t, errors.Is(err, gating.ErrRuleNotFound), "want ErrRuleNotFound, got %v", err)
}

// An empty actor must never be treated as "owns everything".
func TestGatingRule_EmptyActorIsForbidden(t *testing.T) {
	_, rules, contentID := newRuleFixture(t)
	if rules == nil {
		return
	}
	ctx := context.Background()

	_, err := rules.CreateRuleForOwner(ctx, ruleFor(contentID), "")
	require.Error(t, err)
	assert.True(t, errors.Is(err, gating.ErrRuleForbidden), "want ErrRuleForbidden, got %v", err)
}

// Wallet addresses are hex and may differ in case; ownership must not depend on
// how the address was capitalised.
func TestGatingRule_OwnerMatchIsCaseInsensitive(t *testing.T) {
	_, rules, contentID := newRuleFixture(t)
	if rules == nil {
		return
	}

	id, err := rules.CreateRuleForOwner(context.Background(), ruleFor(contentID),
		"0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	require.NoError(t, err, "a lower-cased owner address must still match")
	assert.NotEmpty(t, id)
}
