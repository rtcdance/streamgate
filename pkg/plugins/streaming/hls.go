package streaming

// HLSGenerator generates HLS playlists
type HLSGenerator struct{}

// Generate generates HLS playlist
func (g *HLSGenerator) Generate(contentID string) (string, error) {
return "#EXTM3U\n#EXT-X-VERSION:3\n", nil
}
