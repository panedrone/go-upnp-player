package stream

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"os"

	"github.com/mewkiz/flac/frame"
	"github.com/mewkiz/flac/meta"
)

// bisectFrame finds the byte offset of a FLAC frame that starts at or before
// sample target, and that frame's first sample, by binary search over frame
// headers: jump to the middle of a byte range, find the next valid frame header
// (sync code + CRC-8), compare its sample number, narrow the range. A few dozen
// header reads replace decoding everything up to the target, which is what a
// file without a SEEKTABLE otherwise needs. The caller decodes forward from the
// returned frame and drops the samples before target.
func bisectFrame(f *os.File, info *meta.StreamInfo, target uint64) (int64, uint64, error) {
	size, err := fileSize(f)
	if err != nil {
		return 0, 0, err
	}
	start, err := audioStart(f)
	if err != nil {
		return 0, 0, err
	}
	bestOff, bestSample := start, uint64(0)
	lo, hi := start, size
	for hi-lo > 64<<10 {
		mid := lo + (hi-lo)/2
		off, sample, err := frameAt(f, mid, size, info)
		if err != nil || sample > target {
			hi = mid
			continue
		}
		bestOff, bestSample = off, sample
		lo = off + 1
	}
	return bestOff, bestSample, nil
}

// frameAt returns the offset and first sample of the first valid frame header
// at or after byte off.
func frameAt(f *os.File, off, size int64, info *meta.StreamInfo) (int64, uint64, error) {
	buf := make([]byte, 64<<10)
	for off < size {
		n, err := f.ReadAt(buf, off)
		if n < 2 {
			if err == nil {
				err = io.EOF
			}
			return 0, 0, err
		}
		for i := 0; i < n-1; i++ {
			if buf[i] != 0xFF || buf[i+1]&0xFE != 0xF8 {
				continue
			}
			cand := off + int64(i)
			fr, err := frame.New(io.NewSectionReader(f, cand, size-cand))
			if err != nil {
				continue // a sync-like byte pair inside audio data
			}
			return cand, frameFirstSample(fr, info), nil
		}
		off += int64(n - 1)
	}
	return 0, 0, io.EOF
}

// frameFirstSample: a fixed-blocksize stream numbers frames, so the first sample
// is frame number × block size. The stream's block size is used rather than the
// frame's, because the last frame is shorter.
func frameFirstSample(fr *frame.Frame, info *meta.StreamInfo) uint64 {
	if fr.HasFixedBlockSize {
		return fr.Num * uint64(info.BlockSizeMax)
	}
	return fr.Num
}

// audioStart returns the offset of the first frame: after the "fLaC" marker and
// all metadata blocks.
func audioStart(f *os.File) (int64, error) {
	var head [4]byte
	if _, err := f.ReadAt(head[:], 0); err != nil {
		return 0, err
	}
	if !bytes.Equal(head[:], []byte("fLaC")) {
		return 0, errors.New("no fLaC marker at the start (ID3 header?)")
	}
	off := int64(4)
	for {
		var bh [4]byte
		if _, err := f.ReadAt(bh[:], off); err != nil {
			return 0, err
		}
		length := int64(binary.BigEndian.Uint32(bh[:]) & 0xFFFFFF)
		off += 4 + length
		if bh[0]&0x80 != 0 { // last metadata block
			return off, nil
		}
	}
}

func fileSize(f *os.File) (int64, error) {
	st, err := f.Stat()
	if err != nil {
		return 0, err
	}
	return st.Size(), nil
}
