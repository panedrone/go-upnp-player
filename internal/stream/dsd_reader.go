package stream

import (
	"fmt"
	"io"
	"log"
	"os"

	"github.com/panedrone/go-upnp-player/internal/library"
	"github.com/panedrone/go-upnp-player/internal/services"
)

// dsdBlock is how many PCM samples one conversion step makes.
const dsdBlock = 4096

// dsdSource converts a DSD file to PCM from a given PCM sample on (ADR-028). The conversion
// starts a little earlier and drops what the filters make while they run in, and their delay,
// so a stream started anywhere gives the same samples as one played from the start.
type dsdSource struct {
	f    *os.File
	d    services.DSDFile
	conv *services.DSDConverter
	bps  int64 // DSD bytes of one channel per PCM sample
	next int64 // the next DSD byte of each channel to convert; below 0 or past the end: silence
	drop int   // converted samples still to drop
	path string
}

func openDSD(span Span, from uint64) (*dsdSource, error) {
	f, err := os.Open(span.Path)
	if err != nil {
		return nil, err
	}
	d := *span.DSD
	s := &dsdSource{
		f: f, d: d, path: span.Path,
		conv: services.NewDSDConverter(d.Rate, span.SampleRate, d.Channels, library.DSD().GainDB),
		bps:  int64(services.DSDBytesPerSample(d.Rate, span.SampleRate)),
	}
	warm := 2*s.conv.Delay() + 64
	s.next = (int64(from) - int64(warm)) * s.bps
	s.drop = warm + s.conv.Delay()
	return s, nil
}

// block converts the next dsdBlock samples of every channel.
func (s *dsdSource) block() ([][]int32, error) {
	for {
		in := make([][]byte, s.d.Channels)
		for ch := range in {
			b, err := s.read(ch, s.next, dsdBlock*s.bps)
			if err != nil {
				return nil, err
			}
			in[ch] = b
		}
		s.next += dsdBlock * s.bps
		out := s.conv.Process(in)
		if s.drop >= len(out[0]) {
			s.drop -= len(out[0])
			continue
		}
		for ch := range out {
			out[ch] = out[ch][s.drop:]
		}
		s.drop = 0
		return out, nil
	}
}

// read returns n DSD bytes of channel ch from byte i on, most significant bit first; what lies
// outside the audio is the DSD silence pattern.
func (s *dsdSource) read(ch int, i, n int64) ([]byte, error) {
	out := make([]byte, n)
	have := int64((s.d.Samples + 7) / 8) // audio bytes of one channel
	for k := int64(0); k < n; {
		at := i + k
		if at < 0 || at >= have {
			out[k] = services.DSDSilence
			k++
			continue
		}
		// one contiguous piece: up to the end of a DSF block, the audio or what is wanted
		run := min(n-k, have-at)
		if s.d.Block > 0 {
			run = min(run, int64(s.d.Block)-at%int64(s.d.Block))
			if _, err := s.f.ReadAt(out[k:k+run], s.d.ByteAt(ch, at)); err != nil && err != io.EOF {
				return nil, fmt.Errorf("dsf %s: %w", s.path, err)
			}
		} else {
			// DSDIFF interleaves bytes: read the channels' bytes together, keep this one's
			buf := make([]byte, run*int64(s.d.Channels))
			if _, err := s.f.ReadAt(buf, s.d.ByteAt(0, at)); err != nil && err != io.EOF {
				return nil, fmt.Errorf("dff %s: %w", s.path, err)
			}
			for j := range run {
				out[k+j] = buf[j*int64(s.d.Channels)+int64(ch)]
			}
		}
		if s.d.LSBFirst {
			for j := k; j < k+run; j++ {
				out[j] = services.ReverseBits[out[j]]
			}
		}
		k += run
	}
	return out, nil
}

func (s *dsdSource) Close() error {
	if s.conv.Clipped > 0 {
		log.Printf("dsd: %d samples clipped in %s; a lower gain in the DSD card avoids it", s.conv.Clipped, s.path)
	}
	return s.f.Close()
}
