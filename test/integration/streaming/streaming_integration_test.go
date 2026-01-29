package streaming_test

import (
	"context"
	"testing"

	"streamgate/pkg/models"
	"streamgate/pkg/service"
	"streamgate/test/helpers"
)

func TestStreamingService_CreateStream(t *testing.T) {
	// Setup
	db := helpers.SetupTestDB(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestDB(t, db)

	streamingService := service.NewStreamingService(db)

	// Create content first
	content := &models.Content{
		Title:       "Test Stream",
		Description: "Test streaming content",
		Type:        "video",
		Duration:    3600,
		FileSize:    1024000,
	}

	// Create stream
	stream, err := streamingService.CreateStream(context.Background(), content)
	helpers.AssertNoError(t, err)
	helpers.AssertNotNil(t, stream)
}

func TestStreamingService_GetStreamURL(t *testing.T) {
	// Setup
	db := helpers.SetupTestDB(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestDB(t, db)

	streamingService := service.NewStreamingService(db)

	// Create content
	content := &models.Content{
		Title:       "Test Stream",
		Description: "Test streaming content",
		Type:        "video",
		Duration:    3600,
		FileSize:    1024000,
	}

	// Create stream
	stream, err := streamingService.CreateStream(context.Background(), content)
	helpers.AssertNoError(t, err)

	// Get stream URL
	url, err := streamingService.GetStreamURL(context.Background(), stream.ID, "hls")
	helpers.AssertNoError(t, err)
	helpers.AssertNotEmpty(t, url)
}

func TestStreamingService_HLSFormat(t *testing.T) {
	// Setup
	db := helpers.SetupTestDB(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestDB(t, db)

	streamingService := service.NewStreamingService(db)

	// Create content
	content := &models.Content{
		Title:       "Test HLS Stream",
		Description: "Test HLS streaming",
		Type:        "video",
		Duration:    3600,
		FileSize:    1024000,
	}

	// Create stream
	stream, err := streamingService.CreateStream(context.Background(), content)
	helpers.AssertNoError(t, err)

	// Get HLS URL
	url, err := streamingService.GetStreamURL(context.Background(), stream.ID, "hls")
	helpers.AssertNoError(t, err)
	helpers.AssertContains(t, url, ".m3u8")
}

func TestStreamingService_DASHFormat(t *testing.T) {
	// Setup
	db := helpers.SetupTestDB(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestDB(t, db)

	streamingService := service.NewStreamingService(db)

	// Create content
	content := &models.Content{
		Title:       "Test DASH Stream",
		Description: "Test DASH streaming",
		Type:        "video",
		Duration:    3600,
		FileSize:    1024000,
	}

	// Create stream
	stream, err := streamingService.CreateStream(context.Background(), content)
	helpers.AssertNoError(t, err)

	// Get DASH URL
	url, err := streamingService.GetStreamURL(context.Background(), stream.ID, "dash")
	helpers.AssertNoError(t, err)
	helpers.AssertContains(t, url, ".mpd")
}

func TestStreamingService_AdaptiveBitrate(t *testing.T) {
	// Setup
	db := helpers.SetupTestDB(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestDB(t, db)

	streamingService := service.NewStreamingService(db)

	// Create content
	content := &models.Content{
		Title:       "Test ABR Stream",
		Description: "Test adaptive bitrate streaming",
		Type:        "video",
		Duration:    3600,
		FileSize:    1024000,
	}

	// Create stream
	stream, err := streamingService.CreateStream(context.Background(), content)
	helpers.AssertNoError(t, err)

	// Get adaptive bitrate variants
	variants, err := streamingService.GetAdaptiveBitrates(context.Background(), stream.ID)
	helpers.AssertNoError(t, err)
	helpers.AssertTrue(t, len(variants) > 0)
}

func TestStreamingService_StopStream(t *testing.T) {
	// Setup
	db := helpers.SetupTestDB(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestDB(t, db)

	streamingService := service.NewStreamingService(db)

	// Create content
	content := &models.Content{
		Title:       "Test Stream",
		Description: "Test streaming content",
		Type:        "video",
		Duration:    3600,
		FileSize:    1024000,
	}

	// Create stream
	stream, err := streamingService.CreateStream(context.Background(), content)
	helpers.AssertNoError(t, err)

	// Stop stream
	err = streamingService.StopStream(context.Background(), stream.ID)
	helpers.AssertNoError(t, err)
}
