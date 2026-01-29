package streaming

// AdaptiveBitrate handles adaptive bitrate streaming
type AdaptiveBitrate struct{}

// SelectBitrate selects appropriate bitrate
func (ab *AdaptiveBitrate) SelectBitrate(bandwidth int) int {
if bandwidth < 1000 {
return 500
} else if bandwidth < 5000 {
return 2500
}
return 5000
}
