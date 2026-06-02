package models

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestAuditLog_Creation(t *testing.T) {
	now := time.Now()
	log := &AuditLog{
		ID:         1,
		Action:     "nft.verify",
		Actor:      "0xABCDEF1234567890ABCDEF1234567890ABCDEF12",
		Resource:   "nft",
		ResourceID: "nft-123",
		Success:    true,
		ErrorMsg:   "",
		Details:    "Verified ERC-721 ownership",
		CreatedAt:  now,
	}

	assert.Equal(t, int64(1), log.ID)
	assert.Equal(t, "nft.verify", log.Action)
	assert.Equal(t, "0xABCDEF1234567890ABCDEF1234567890ABCDEF12", log.Actor)
	assert.Equal(t, "nft", log.Resource)
	assert.Equal(t, "nft-123", log.ResourceID)
	assert.True(t, log.Success)
	assert.Equal(t, "", log.ErrorMsg)
	assert.Equal(t, "Verified ERC-721 ownership", log.Details)
}

func TestAuditLog_FailedAction(t *testing.T) {
	log := &AuditLog{
		ID:         2,
		Action:     "auth.login",
		Actor:      "0xBAD",
		Resource:   "auth",
		ResourceID: "session-456",
		Success:    false,
		ErrorMsg:   "invalid signature",
		Details:    "Wallet signature verification failed",
	}

	assert.False(t, log.Success)
	assert.Equal(t, "invalid signature", log.ErrorMsg)
}

func TestAuditLog_ZeroValues(t *testing.T) {
	log := &AuditLog{}

	assert.Equal(t, int64(0), log.ID)
	assert.Equal(t, "", log.Action)
	assert.Equal(t, "", log.Actor)
	assert.False(t, log.Success)
	assert.True(t, log.CreatedAt.IsZero())
}

func TestAuditLog_JSONMarshaling(t *testing.T) {
	log := &AuditLog{
		ID:         3,
		Action:     "content.upload",
		Actor:      "user-1",
		Resource:   "content",
		ResourceID: "content-789",
		Success:    true,
		Details:    "File uploaded",
	}

	data, err := json.Marshal(log)
	assert.NoError(t, err)

	var decoded AuditLog
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)
	assert.Equal(t, log.ID, decoded.ID)
	assert.Equal(t, log.Action, decoded.Action)
	assert.Equal(t, log.Success, decoded.Success)
}

func TestCategory_Creation(t *testing.T) {
	now := time.Now()
	cat := &Category{
		ID:          "cat-1",
		Name:        "Music Videos",
		Slug:        "music-videos",
		Description: "Music video content",
		ParentID:    "",
		CreatedAt:   now,
	}

	assert.Equal(t, "cat-1", cat.ID)
	assert.Equal(t, "Music Videos", cat.Name)
	assert.Equal(t, "music-videos", cat.Slug)
	assert.Equal(t, "Music video content", cat.Description)
	assert.Equal(t, "", cat.ParentID)
}

func TestCategory_Subcategory(t *testing.T) {
	cat := &Category{
		ID:       "cat-2",
		Name:     "Rock",
		Slug:     "rock",
		ParentID: "cat-1",
	}

	assert.Equal(t, "cat-1", cat.ParentID)
	assert.NotEmpty(t, cat.ParentID)
}

func TestCategory_ZeroValues(t *testing.T) {
	cat := &Category{}

	assert.Equal(t, "", cat.ID)
	assert.Equal(t, "", cat.Name)
	assert.Equal(t, "", cat.Slug)
	assert.Equal(t, "", cat.ParentID)
	assert.True(t, cat.CreatedAt.IsZero())
}

func TestCategory_JSONMarshaling(t *testing.T) {
	cat := &Category{
		ID:          "json-cat",
		Name:        "Gaming",
		Slug:        "gaming",
		Description: "Gaming content",
	}

	data, err := json.Marshal(cat)
	assert.NoError(t, err)

	var decoded Category
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)
	assert.Equal(t, cat.ID, decoded.ID)
	assert.Equal(t, cat.Name, decoded.Name)
	assert.Equal(t, cat.Slug, decoded.Slug)
}

func TestGatingRule_Creation(t *testing.T) {
	now := time.Now()
	rule := &GatingRule{
		ID:              "rule-1",
		ContentID:       "content-123",
		ContractAddress: "0x1234567890123456789012345678901234567890",
		TokenID:         "1",
		ChainID:         1,
		Standard:        "erc721",
		MinBalance:      1,
		IsActive:        true,
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	assert.Equal(t, "rule-1", rule.ID)
	assert.Equal(t, "content-123", rule.ContentID)
	assert.Equal(t, "0x1234567890123456789012345678901234567890", rule.ContractAddress)
	assert.Equal(t, "1", rule.TokenID)
	assert.Equal(t, int64(1), rule.ChainID)
	assert.Equal(t, "erc721", rule.Standard)
	assert.Equal(t, 1, rule.MinBalance)
	assert.True(t, rule.IsActive)
}

func TestGatingRule_Inactive(t *testing.T) {
	rule := &GatingRule{
		ID:         "rule-2",
		IsActive:   false,
		MinBalance: 0,
	}

	assert.False(t, rule.IsActive)
	assert.Equal(t, 0, rule.MinBalance)
}

func TestGatingRule_ZeroValues(t *testing.T) {
	rule := &GatingRule{}

	assert.Equal(t, "", rule.ID)
	assert.Equal(t, "", rule.ContentID)
	assert.Equal(t, int64(0), rule.ChainID)
	assert.False(t, rule.IsActive)
	assert.True(t, rule.CreatedAt.IsZero())
}

func TestGatingRule_JSONMarshaling(t *testing.T) {
	rule := &GatingRule{
		ID:              "json-rule",
		ContentID:       "content-json",
		ContractAddress: "0xABC",
		ChainID:         137,
		Standard:        "erc1155",
		MinBalance:      5,
		IsActive:        true,
	}

	data, err := json.Marshal(rule)
	assert.NoError(t, err)

	var decoded GatingRule
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)
	assert.Equal(t, rule.ID, decoded.ID)
	assert.Equal(t, rule.ChainID, decoded.ChainID)
	assert.Equal(t, rule.MinBalance, decoded.MinBalance)
	assert.Equal(t, rule.IsActive, decoded.IsActive)
}

func TestGatingRuleStandard_Constants(t *testing.T) {
	assert.Equal(t, GatingRuleStandard("erc721"), GatingStandardERC721)
	assert.Equal(t, GatingRuleStandard("erc1155"), GatingStandardERC1155)
	assert.Equal(t, GatingRuleStandard("any"), GatingStandardAny)
}

func TestPlaybackEvent_Creation(t *testing.T) {
	now := time.Now()
	event := &PlaybackEvent{
		ID:               "pe-1",
		ContentID:        "content-123",
		WalletAddress:    "0xABC",
		EventType:        "start",
		DurationSeconds:  30,
		PlaybackTokenJTI: "jti-abc123",
		UserAgent:        "Mozilla/5.0",
		IPAddress:        "192.168.1.1",
		CreatedAt:        now,
	}

	assert.Equal(t, "pe-1", event.ID)
	assert.Equal(t, "content-123", event.ContentID)
	assert.Equal(t, "0xABC", event.WalletAddress)
	assert.Equal(t, "start", event.EventType)
	assert.Equal(t, 30, event.DurationSeconds)
	assert.Equal(t, "jti-abc123", event.PlaybackTokenJTI)
}

func TestPlaybackEvent_ZeroValues(t *testing.T) {
	event := &PlaybackEvent{}

	assert.Equal(t, "", event.ID)
	assert.Equal(t, 0, event.DurationSeconds)
	assert.True(t, event.CreatedAt.IsZero())
}

func TestPlaybackEvent_JSONMarshaling(t *testing.T) {
	event := &PlaybackEvent{
		ID:            "json-pe",
		ContentID:     "content-json",
		WalletAddress: "0xDEF",
		EventType:     "segment",
	}

	data, err := json.Marshal(event)
	assert.NoError(t, err)

	var decoded PlaybackEvent
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)
	assert.Equal(t, event.ID, decoded.ID)
	assert.Equal(t, event.EventType, decoded.EventType)
}

func TestPlaybackEventType_Constants(t *testing.T) {
	assert.Equal(t, PlaybackEventType("start"), PlaybackEventStart)
	assert.Equal(t, PlaybackEventType("segment"), PlaybackEventSegment)
	assert.Equal(t, PlaybackEventType("end"), PlaybackEventEnd)
}

func TestContentStats_Creation(t *testing.T) {
	now := time.Now()
	stats := &ContentStats{
		ContentID:         "content-123",
		TotalPlays:        1000,
		UniqueViewers:     500,
		TotalWatchSeconds: 36000,
		AvgWatchSeconds:   72,
		UpdatedAt:         now,
	}

	assert.Equal(t, "content-123", stats.ContentID)
	assert.Equal(t, 1000, stats.TotalPlays)
	assert.Equal(t, 500, stats.UniqueViewers)
	assert.Equal(t, int64(36000), stats.TotalWatchSeconds)
	assert.Equal(t, 72, stats.AvgWatchSeconds)
}

func TestContentStats_ZeroValues(t *testing.T) {
	stats := &ContentStats{}

	assert.Equal(t, "", stats.ContentID)
	assert.Equal(t, 0, stats.TotalPlays)
	assert.Equal(t, 0, stats.UniqueViewers)
	assert.Equal(t, int64(0), stats.TotalWatchSeconds)
	assert.True(t, stats.UpdatedAt.IsZero())
}

func TestContentStats_JSONMarshaling(t *testing.T) {
	stats := &ContentStats{
		ContentID:         "json-stats",
		TotalPlays:        100,
		UniqueViewers:     50,
		TotalWatchSeconds: 5000,
		AvgWatchSeconds:   50,
	}

	data, err := json.Marshal(stats)
	assert.NoError(t, err)

	var decoded ContentStats
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)
	assert.Equal(t, stats.ContentID, decoded.ContentID)
	assert.Equal(t, stats.TotalPlays, decoded.TotalPlays)
	assert.Equal(t, stats.TotalWatchSeconds, decoded.TotalWatchSeconds)
}
