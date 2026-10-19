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
			if value == "" || strings.Count(value, ".") > 1 || strings.IndexFunc(value, func(r rune) bool {
				return (r < '0' || r > '9') && r != '.'
			}) >= 0 {
				return nil, fmt.Errorf("invalid EXTINF duration")
			}
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
		mediaURL := resolveURI(cfg.BaseURL, next.uri)
		sample, mediaBody := fetchURL(ctx, client, mediaURL, jwt, cfg)
		// Segment identity comes from EXTINF, so a manifest-looking URI cannot
		// turn a media request into an unchecked playlist request.
		if sample.ok() && !sample.HashVerified {
			if err := verifyMediaHash(mediaURL, mediaBody, cfg.MediaSHA256); err != nil {
				sample.MediaHashError = true
				sample.Err = err.Error()
			} else {
				sample.HashVerified = true
			}
		}
		sample.ViewerID = viewerID
		rec.record(sample)
		if !waitPlayback(ctx, next.duration-time.Since(started)) {
			return
		}
	}
}
