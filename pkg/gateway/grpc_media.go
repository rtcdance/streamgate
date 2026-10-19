package gateway

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	streamingv1 "github.com/rtcdance/streamgate/pkg/api/v1/streaming"
	"github.com/rtcdance/streamgate/pkg/service"
	"github.com/rtcdance/streamgate/pkg/service/streamingsvc"
)

// grpcStoredManifest keeps FFmpeg's timing and rendition metadata. Every listed
// segment is constrained to the resolved generation and the media playlist;
// filenames supplied by a playlist cannot introduce another object namespace.
func grpcStoredManifest(ctx context.Context, store service.SegmentStorage, contentID, token string, qualities map[string][]string, playlists map[string]string, master string) (string, []*streamingv1.SegmentInfo, error) {
	names := make([]string, 0, len(playlists))
	for quality := range playlists {
		names = append(names, quality)
	}
	sort.Strings(names)
	segments := make([]*streamingv1.SegmentInfo, 0)
	manifest := ""
	for _, quality := range names {
		raw, err := store.Download(ctx, "streamgate", playlists[quality])
		if err != nil {
			return "", nil, fmt.Errorf("stored media playlist unavailable")
		}
		rewritten, err := streamingsvc.RewriteMediaPlaylistURIs(string(raw), contentID, quality, token)
		if err != nil {
			return "", nil, fmt.Errorf("stored media playlist unsafe")
		}
		entries, err := grpcPlaylistSegments(string(raw), contentID, quality, token, qualities[quality])
		if err != nil {
			return "", nil, err
		}
		segments = append(segments, entries...)
		manifest = rewritten
	}
	if master != "" {
		raw, err := store.Download(ctx, "streamgate", master)
		if err != nil {
			return "", nil, fmt.Errorf("stored master playlist unavailable")
		}
		manifest, err = streamingsvc.RewriteMasterPlaylistURIs(string(raw), contentID, token)
		if err != nil {
			return "", nil, fmt.Errorf("stored master playlist unsafe")
		}
	} else if len(names) != 1 {
		return "", nil, fmt.Errorf("stored master playlist missing")
	}
	if len(segments) == 0 {
		return "", nil, fmt.Errorf("stored media playlist empty")
	}
	return manifest, segments, nil
}

func grpcPlaylistSegments(raw, contentID, quality, token string, listed []string) ([]*streamingv1.SegmentInfo, error) {
	if len(raw) > 1<<20 || !strings.HasPrefix(raw, "#EXTM3U") {
		return nil, fmt.Errorf("invalid media playlist")
	}
	allowed := make(map[string]bool, len(listed))
	for _, name := range listed {
		allowed[name] = true
	}
	segments := make([]*streamingv1.SegmentInfo, 0)
	duration := float64(0)
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#EXTINF:") {
			if duration != 0 {
				return nil, fmt.Errorf("media duration without segment")
			}
			value, _, _ := strings.Cut(strings.TrimPrefix(line, "#EXTINF:"), ",")
			parsed, err := strconv.ParseFloat(value, 64)
			if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) || parsed <= 0 || parsed > 86400 {
				return nil, fmt.Errorf("invalid media duration")
			}
			duration = parsed
		} else if line != "" && !strings.HasPrefix(line, "#") {
			id := line
			if quality != "default" {
				id = quality + "/" + line
			}
			if duration == 0 || !allowed[line] || !validGRPCSegmentID(id) {
				return nil, fmt.Errorf("media segment outside published playlist")
			}
			segments = append(segments, &streamingv1.SegmentInfo{
				SegmentId: id, Quality: quality,
				// The wire field is integer seconds. HLS keeps the exact fractional value.
				Duration: int64(math.Ceil(duration)),
				Url:      fmt.Sprintf("/api/v1/streaming/%s/segment/%s?quality=%s&playback_token=%s", contentID, line, quality, token),
			})
			duration = 0
		}
	}
	if duration != 0 || len(segments) == 0 {
		return nil, fmt.Errorf("incomplete media playlist")
	}
	return segments, nil
}

func grpcPublishedMediaIndex(ctx context.Context, store service.SegmentStorage, segmentPrefix string, generation int64) (qualitySegments map[string][]string, playlists map[string]string, master string, err error) {
	qualitySegments = make(map[string][]string)
	playlists = make(map[string]string)
	if store == nil {
		return qualitySegments, playlists, master, nil
	}
	objs, err := store.ListObjects(ctx, "streamgate", segmentPrefix)
	if err != nil {
		return nil, nil, "", fmt.Errorf("published media listing unavailable")
	}
	for _, key := range objs {
		if !strings.HasPrefix(key, segmentPrefix) {
			continue
		}
		rel := strings.TrimPrefix(key, segmentPrefix)
		parts := strings.SplitN(rel, "/", 2)
		quality := "default"
		segName := rel
		if len(parts) == 2 {
			quality = parts[0]
			segName = parts[1]
			// A legacy listing must never index objects that live inside
			// a generation-private directory.
			if generation == 0 && isGenerationDirName(quality) {
				continue
			}
		}
		if rel == "master.m3u8" {
			master = key
			continue
		}
		if !isValidRenditionName(quality) || isGenerationDirName(quality) || strings.ContainsAny(segName, "/\\") {
			continue
		}
		if strings.HasSuffix(segName, ".m3u8") {
			if _, duplicate := playlists[quality]; duplicate {
				return nil, nil, "", fmt.Errorf("ambiguous stored playlist")
			}
			playlists[quality] = key
		} else if validGRPCSegmentID(rel) {
			qualitySegments[quality] = append(qualitySegments[quality], segName)
		}
	}
	return qualitySegments, playlists, master, nil
}
