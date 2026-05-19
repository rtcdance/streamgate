package main

import (
	"fmt"
	"strings"
	"time"
)

// HLS (HTTP Live Streaming) breaks video into small 4-10 second segments.
// The manifest (.m3u8) lists these segments; the player downloads and
// plays them sequentially. This demo shows the manifest generation logic.
//
// In production, StreamGate uses NFT ownership to gate access to the
// manifest and generates per-user playback tokens to prevent sharing.

func main() {
	contentID := "demo-video-001"
	segments := []string{"seg001.ts", "seg002.ts", "seg003.ts", "seg004.ts"}
	duration := 10 // seconds per segment

	fmt.Println("=== HLS Streaming Demo ===")
	fmt.Println()
	fmt.Println("Content ID:", contentID)
	fmt.Println()

	// Step 1: Generate an HLS manifest (.m3u8 playlist)
	manifest := generateManifest(contentID, segments, duration)
	fmt.Println("--- Manifest (playlist.m3u8) ---")
	fmt.Println(manifest)

	// Step 2: Simulate segment delivery
	playbackToken := generateToken(contentID)
	fmt.Println("--- Segment Delivery ---")
	for _, seg := range segments {
		fmt.Printf("GET /api/v1/streaming/%s/segment/%s?token=%s\n",
			contentID, seg, playbackToken)
		time.Sleep(50 * time.Millisecond) // simulate network
	}
	fmt.Println()
	fmt.Println("✅ Streaming demo complete")
	fmt.Println()
	fmt.Println("── What just happened ──")
	fmt.Println("1. Server generated an HLS manifest listing 4 video segments")
	fmt.Println("2. Each segment URL is gated behind a playback token")
	fmt.Println("3. In production, the token is bound to wallet + content + contract")
	fmt.Println("4. Without a valid token, the server returns 403 Forbidden")
	fmt.Println()
	fmt.Println("📖 Next: read pkg/service/streaming.go for the real implementation")
}

func generateManifest(contentID string, segments []string, segDuration int) string {
	var b strings.Builder
	b.WriteString("#EXTM3U\n")
	b.WriteString("#EXT-X-VERSION:3\n")
	b.WriteString("#EXT-X-TARGETDURATION:10\n")
	b.WriteString(fmt.Sprintf("#EXT-X-MEDIA-SEQUENCE:0\n"))
	for _, seg := range segments {
		b.WriteString(fmt.Sprintf("#EXTINF:%d,\n", segDuration))
		b.WriteString(fmt.Sprintf("%s\n", seg))
	}
	b.WriteString("#EXT-X-ENDLIST\n")
	return b.String()
}

func generateToken(contentID string) string {
	return fmt.Sprintf("playback_%s_%d", contentID, time.Now().Unix())
}
