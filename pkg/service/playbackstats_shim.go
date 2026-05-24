package service

import "github.com/rtcdance/streamgate/pkg/service/playbackstats"

type (
	PlaybackStatsService = playbackstats.PlaybackStatsService
)

var (
	NewPlaybackStatsService = playbackstats.NewPlaybackStatsService
)
