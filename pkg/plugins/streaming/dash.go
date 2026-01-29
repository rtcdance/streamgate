package streaming

// DASHGenerator generates DASH manifests
type DASHGenerator struct{}

// Generate generates DASH manifest
func (g *DASHGenerator) Generate(contentID string) (string, error) {
return `<?xml version="1.0"?><MPD></MPD>`, nil
}
