package transcoder

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// fakeHLSEngine records what the adapter asked the FFmpeg engine to do, so the
// adapter's mapping is pinned without spawning FFmpeg.
type fakeHLSEngine struct {
	hlsCalled        bool
	hlsProfile       string
	hlsDeadline      time.Time
	hlsHasDeadline   bool
	toHLSCalled      bool
	toHLSProfiles    []TranscodeProfile
	toHLSDeadline    time.Time
	toHLSHasDeadline bool
}

func (f *fakeHLSEngine) TranscodeHLS(ctx context.Context, _, _, profile string, _ func(variant string, progress float64)) error {
	f.hlsCalled = true
	f.hlsProfile = profile
	f.hlsDeadline, f.hlsHasDeadline = ctx.Deadline()
	return nil
}

func (f *fakeHLSEngine) TranscodeToHLS(ctx context.Context, _, _ string, profiles []TranscodeProfile, _ ProgressCallback, _ func(variant string, progress float64)) error {
	f.toHLSCalled = true
	f.toHLSProfiles = profiles
	f.toHLSDeadline, f.toHLSHasDeadline = ctx.Deadline()
	return nil
}

// TestVideoTranscoderAdapter_DeadlineWrap pins the 30-minute deadline wrap the
// gateway handler relies on: a context without a deadline gets one, a context
// with a deadline passes through untouched.
func TestVideoTranscoderAdapter_DeadlineWrap(t *testing.T) {
	t.Run("no deadline gets the 30 minute wrap", func(t *testing.T) {
		fake := &fakeHLSEngine{}
		a := &VideoTranscoderAdapter{ft: fake, log: zap.NewNop()}

		before := time.Now()
		require.NoError(t, a.TranscodeHLS(context.Background(), "in.mp4", "out", "abr", nil))
		require.True(t, fake.hlsHasDeadline, "the engine must receive a bounded context")
		remaining := time.Until(fake.hlsDeadline)
		assert.Greater(t, remaining, 29*time.Minute)
		assert.LessOrEqual(t, remaining, 30*time.Minute)
		assert.False(t, fake.hlsDeadline.Before(before.Add(29*time.Minute)))
	})

	t.Run("existing deadline is preserved", func(t *testing.T) {
		fake := &fakeHLSEngine{}
		a := &VideoTranscoderAdapter{ft: fake, log: zap.NewNop()}

		want := time.Now().Add(90 * time.Second)
		ctx, cancel := context.WithDeadline(context.Background(), want)
		defer cancel()
		require.NoError(t, a.TranscodeHLS(ctx, "in.mp4", "out", "abr", nil))
		require.True(t, fake.hlsHasDeadline)
		assert.WithinDuration(t, want, fake.hlsDeadline, time.Millisecond,
			"an explicit caller deadline must not be widened")
	})
}

// TestVideoTranscoderAdapter_ProfileFallback pins the profile mapping:
// abr/"" delegate to the engine's own selection, known profiles become a
// single HLS variant, anything else falls back to the full ladder.
func TestVideoTranscoderAdapter_ProfileFallback(t *testing.T) {
	cases := []struct {
		name          string
		profile       string
		wantDelegated bool
		wantRes       []string
	}{
		{name: "abr delegates to engine", profile: "abr", wantDelegated: true},
		{name: "empty profile delegates to engine", profile: "", wantDelegated: true},
		{name: "1080p maps to one variant", profile: "1080p", wantRes: []string{"1920x1080"}},
		{name: "720p maps to one variant", profile: "720p", wantRes: []string{"1280x720"}},
		{name: "480p maps to one variant", profile: "480p", wantRes: []string{"854x480"}},
		{name: "360p maps to one variant", profile: "360p", wantRes: []string{"640x360"}},
		{name: "unknown profile falls back to the ladder", profile: "potato",
			wantRes: []string{"1920x1080", "1280x720", "854x480", "640x360"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeHLSEngine{}
			a := &VideoTranscoderAdapter{ft: fake, log: zap.NewNop()}

			require.NoError(t, a.TranscodeHLS(context.Background(), "in.mp4", "out", tc.profile, nil))
			if tc.wantDelegated {
				assert.True(t, fake.hlsCalled, "must delegate to FFmpegTranscoder.TranscodeHLS")
				assert.False(t, fake.toHLSCalled)
				assert.Equal(t, tc.profile, fake.hlsProfile)
				return
			}
			require.True(t, fake.toHLSCalled, "must go through TranscodeToHLS")
			assert.False(t, fake.hlsCalled)
			var got []string
			for _, p := range fake.toHLSProfiles {
				got = append(got, p.Resolution)
				assert.Equal(t, "hls", p.Format)
			}
			assert.Equal(t, tc.wantRes, got)
		})
	}
}
