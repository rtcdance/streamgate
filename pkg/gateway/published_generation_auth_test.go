package gateway

import (
	"context"
	"errors"
	"math/big"
	"net/http"
	"net/url"
	"testing"
	"time"

	streamingv1 "github.com/rtcdance/streamgate/pkg/api/v1/streaming"
	"github.com/rtcdance/streamgate/pkg/service"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestPublishedGeneration_AuthorityFailureNeverServesLegacy(t *testing.T) {
	for _, kind := range []string{"database-error", "negative-generation"} {
		resolver := PublishedGenerationResolver(func(context.Context, string) (int64, string, error) {
			if kind == "negative-generation" {
				return -1, "", nil
			}
			return 0, "", errors.New("authority unavailable")
		})
		t.Run(kind+"/http", func(t *testing.T) {
			f := newGenPublishFixture(t)
			f.store.put("streams/c1/seg0.ts", "LEGACY-MUST-NOT-SERVE")
			f.cache.SetGenerationResolver(resolver)
			token := f.token(t, "c1")
			for _, route := range []string{genManifestPath("c1"), genSegmentPath("c1", "seg0.ts", "", token)} {
				response := f.get(t, route)
				require.Equal(t, http.StatusServiceUnavailable, response.Code)
				require.NotContains(t, response.Body.String(), "LEGACY-MUST-NOT-SERVE")
			}
		})
		t.Run(kind+"/grpc", func(t *testing.T) {
			svc := service.NewAuthService(grpcAuthzSecret, nil)
			session := startTestSession(t, svc)
			token, err := svc.GeneratePlaybackToken(context.Background(), "0xabc", "c1", "", "", 1, time.Minute, "", service.WithSessionBinding(session, 1))
			require.NoError(t, err)
			ctx := bindTestGRPCCapabilitySession(t, parityCtx(session, 1, "0xabc"), svc, token)
			srv, _ := newParityServer(t, big.NewInt(5), 1, svc)
			srv.genResolver = newGenerationResolver(resolver)
			_, err = srv.GetManifest(ctx, &streamingv1.GetManifestRequest{ContentId: "c1"})
			require.Equal(t, codes.Unavailable, status.Code(err), "%v", err)
			ctx = metadata.NewIncomingContext(ctx, metadata.Pairs("playback-token", token))
			response, err := srv.GetSegment(ctx, &streamingv1.GetSegmentRequest{ContentId: "c1", SegmentId: "seg0.ts"})
			require.Equal(t, codes.Unavailable, status.Code(err), "%v", err)
			require.Nil(t, response)
		})
	}
}

func TestGRPC_PublishedRenditionSegmentRoundTrip(t *testing.T) {
	svc := service.NewAuthService(grpcAuthzSecret, nil)
	svc.SetRuleVersionReader(func(context.Context, string) (int64, error) { return 1, nil })
	session := startTestSession(t, svc)
	restore, ok := svc.RestoreEpoch()
	require.True(t, ok)
	srv, _ := newParityServer(t, big.NewInt(5), 1, svc)
	store := newGrpcMockSegmentStorage()
	store.list = []string{"streams/c1/g2/720p/seg0.ts", "streams/c1/g2/720p/index.m3u8"}
	require.NoError(t, store.Upload(context.Background(), "streamgate", store.list[1], []byte("#EXTM3U\n#EXT-X-TARGETDURATION:1\n#EXTINF:0.5,\nseg0.ts\n#EXT-X-ENDLIST\n")))
	require.NoError(t, store.Upload(context.Background(), "streamgate", store.list[0], []byte("PUBLISHED-MEDIA")))
	require.NoError(t, store.Upload(context.Background(), "streamgate", "streams/c1/g3/720p/seg0.ts", []byte("UNPUBLISHED-MEDIA")))
	srv.segStore = store
	srv.genResolver = newGenerationResolver(func(context.Context, string) (int64, string, error) { return 2, "720p", nil })
	ctx := context.WithValue(parityCtx(session, 1, "0xabc"), grpcRestoreEpochKey, restore)
	manifest, err := srv.GetManifest(ctx, &streamingv1.GetManifestRequest{ContentId: "c1"})
	require.NoError(t, err)
	require.Len(t, manifest.Segments, 1)
	require.Contains(t, manifest.Manifest, "#EXTINF:0.5,")
	require.Equal(t, int64(1), manifest.Segments[0].Duration)
	segment := manifest.Segments[0]
	require.Equal(t, "720p/seg0.ts", segment.SegmentId)
	parsed, err := url.Parse(segment.Url)
	require.NoError(t, err)
	require.Equal(t, "720p", parsed.Query().Get("quality"))
	ctx = metadata.NewIncomingContext(ctx, metadata.Pairs("playback-token", parsed.Query().Get("playback_token")))
	response, err := srv.GetSegment(ctx, &streamingv1.GetSegmentRequest{ContentId: "c1", SegmentId: segment.SegmentId})
	require.NoError(t, err)
	require.Equal(t, []byte("PUBLISHED-MEDIA"), response.Data)
	for _, bad := range []string{"g3/seg0.ts", "720p/../seg0.ts", "720p/g3/seg0.ts", "/720p/seg0.ts", "720p//seg0.ts"} {
		t.Run(bad, func(t *testing.T) {
			response, err := srv.GetSegment(ctx, &streamingv1.GetSegmentRequest{ContentId: "c1", SegmentId: bad})
			require.Equal(t, codes.InvalidArgument, status.Code(err))
			require.Nil(t, response)
		})
	}
}

func TestGRPC_PlaylistMetadataRejectsInvalidInput(t *testing.T) {
	for _, body := range []string{
		"#EXTM3U\n#EXTINF:NaN,\nseg0.ts\n",
		"#EXTM3U\n#EXTINF:+Inf,\nseg0.ts\n",
		"#EXTM3U\n#EXTINF:0,\nseg0.ts\n",
		"#EXTM3U\n#EXTINF:1,\n../seg0.ts\n",
		"#EXTM3U\n#EXTINF:1,\ng3/seg0.ts\n",
		"#EXTM3U\n#EXTINF:1,\nmissing.ts\n",
		"#EXTM3U\nseg0.ts\n",
		"#EXTM3U\n#EXTINF:1,\n",
		"#EXTM3U\n#EXTINF:1,\n#EXTINF:2,\nseg0.ts\n",
	} {
		_, err := grpcPlaylistSegments(body, "c1", "720p", "synthetic-token", []string{"seg0.ts"})
		require.Error(t, err)
	}
	entries, err := grpcPlaylistSegments("#EXTM3U\n#EXTINF:10.25,\nseg0.ts\n#EXTINF:0.5,\nseg1.ts\n#EXT-X-ENDLIST\n", "c1", "720p", "synthetic-token", []string{"seg0.ts", "seg1.ts"})
	require.NoError(t, err)
	require.Len(t, entries, 2)
	require.Equal(t, int64(11), entries[0].Duration)
	require.Equal(t, int64(1), entries[1].Duration)
}
