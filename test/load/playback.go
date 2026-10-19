package load

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type playbackSegment struct {
	uri      string
	duration time.Duration
}

func parsePlaybackSegments(body []byte) ([]playbackSegment, error) {
	var segments []playbackSegment
	var duration time.Duration
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#EXTINF:") {
			if duration != 0 {
				return nil, fmt.Errorf("EXTINF without segment")
			}
			value := strings.SplitN(strings.TrimPrefix(line, "#EXTINF:"), ",", 2)[0]
			parsed, err := time.ParseDuration(value + "s")
			if err != nil || parsed <= 0 {
				return nil, fmt.Errorf("invalid EXTINF duration")
			}
			duration = parsed
		} else if line != "" && !strings.HasPrefix(line, "#") {
			if duration <= 0 {
				return nil, fmt.Errorf("expected a rendition segment with EXTINF")
			}
			segments = append(segments, playbackSegment{uri: line, duration: duration})
			duration = 0
		}
	}
	if len(segments) == 0 || duration != 0 {
		return nil, fmt.Errorf("empty or incomplete rendition playlist")
	}
	return segments, nil
}

func waitPlayback(ctx context.Context, delay time.Duration) bool {
	if delay <= 0 {
		return ctx.Err() == nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func playbackLoop(ctx context.Context, client *http.Client, cfg Config, jwt string, viewerID int, rec *recorder) {
	segmentIndex := 0
	for ctx.Err() == nil {
		started := time.Now()
		manifest, body := fetchURL(ctx, client, cfg.manifestURL(), jwt, cfg)
		manifest.ViewerID = viewerID
		var segments []playbackSegment
		if manifest.ok() {
			var err error
			segments, err = parsePlaybackSegments(body)
			if err != nil {
				manifest.Err = "invalid playback playlist: " + err.Error()
			}
		}
		rec.record(manifest)
		if !manifest.ok() {
			if !waitPlayback(ctx, cfg.Interval) {
				return
			}
			continue
		}
		next := segments[segmentIndex%len(segments)]
		segmentIndex++
		sample, _ := fetchURL(ctx, client, resolveURI(cfg.BaseURL, next.uri), jwt, cfg)
		sample.ViewerID = viewerID
		rec.record(sample)
		if !waitPlayback(ctx, next.duration-time.Since(started)) {
			return
		}
	}
}
