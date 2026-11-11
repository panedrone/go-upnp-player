package services

import "math"

// AvgBitrateKbps is the average bitrate of a file in kbit/s (1000 bit), rounded:
// its size over its length. 0 when the length is unknown.
func AvgBitrateKbps(fileSize int64, frames uint64, sampleRate uint32) int {
	if frames == 0 || sampleRate == 0 || fileSize <= 0 {
		return 0
	}
	sec := float64(frames) / float64(sampleRate)
	return int(math.Round(float64(fileSize) * 8 / sec / 1000))
}
