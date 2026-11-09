package services

import "encoding/binary"

// WavHeaderLen is the size of a canonical RIFF/WAVE header with a single data chunk.
const WavHeaderLen = 44

// WavLayout is the geometry of a virtual WAV file cut from a FLAC.
type WavLayout struct {
	SampleRate uint32
	Channels   uint16
	// SourceBits is the source bit depth (FLAC can be 12/20 bits),
	// ContainerBits is the bit depth in the WAV (a multiple of 8).
	SourceBits    uint8
	ContainerBits uint16
	Samples       uint64
}

// NewWavLayout computes the geometry for the given number of samples.
func NewWavLayout(sampleRate uint32, channels uint8, bits uint8, samples uint64) WavLayout {
	container := (uint16(bits) + 7) / 8 * 8
	return WavLayout{
		SampleRate:    sampleRate,
		Channels:      uint16(channels),
		SourceBits:    bits,
		ContainerBits: container,
		Samples:       samples,
	}
}

// BlockAlign is the number of bytes per interleaved sample frame.
func (l WavLayout) BlockAlign() int64 {
	return int64(l.Channels) * int64(l.ContainerBits/8)
}

// DataLen is the length of the PCM data.
func (l WavLayout) DataLen() int64 {
	return int64(l.Samples) * l.BlockAlign()
}

// TotalLen is the full file size including the header.
func (l WavLayout) TotalLen() int64 {
	return WavHeaderLen + l.DataLen()
}

// Header returns the 44-byte header.
func (l WavLayout) Header() []byte {
	h := make([]byte, WavHeaderLen)
	dataLen := l.DataLen()
	copy(h[0:], "RIFF")
	binary.LittleEndian.PutUint32(h[4:], uint32(36+dataLen))
	copy(h[8:], "WAVE")
	copy(h[12:], "fmt ")
	binary.LittleEndian.PutUint32(h[16:], 16)
	binary.LittleEndian.PutUint16(h[20:], 1) // PCM
	binary.LittleEndian.PutUint16(h[22:], l.Channels)
	binary.LittleEndian.PutUint32(h[24:], l.SampleRate)
	binary.LittleEndian.PutUint32(h[28:], l.SampleRate*uint32(l.BlockAlign()))
	binary.LittleEndian.PutUint16(h[32:], uint16(l.BlockAlign()))
	binary.LittleEndian.PutUint16(h[34:], l.ContainerBits)
	copy(h[36:], "data")
	binary.LittleEndian.PutUint32(h[40:], uint32(dataLen))
	return h
}

// LocateOffset maps a byte offset in the virtual WAV to a position in the data:
// the sample index (relative to the track start) and how many bytes of that sample to skip.
// inHeader=true means the offset is inside the header; data starts at sample 0.
func (l WavLayout) LocateOffset(off int64) (sample uint64, skip int64, inHeader bool) {
	if off < WavHeaderLen {
		return 0, 0, true
	}
	data := off - WavHeaderLen
	ba := l.BlockAlign()
	return uint64(data / ba), data % ba, false
}

// AppendSamples appends interleaved samples to dst in WAV format (little-endian).
// channels[ch][i] is a sample of channel ch. Samples from the range [from, to).
func (l WavLayout) AppendSamples(dst []byte, channels [][]int32, from, to int) []byte {
	shift := uint(l.ContainerBits) - uint(l.SourceBits)
	bytesPer := int(l.ContainerBits / 8)
	for i := from; i < to; i++ {
		for ch := range channels {
			v := channels[ch][i] << shift
			switch bytesPer {
			case 1:
				dst = append(dst, byte(v+128)) // 8-bit WAV is unsigned
			case 2:
				dst = append(dst, byte(v), byte(v>>8))
			case 3:
				dst = append(dst, byte(v), byte(v>>8), byte(v>>16))
			default:
				dst = append(dst, byte(v), byte(v>>8), byte(v>>16), byte(v>>24))
			}
		}
	}
	return dst
}
