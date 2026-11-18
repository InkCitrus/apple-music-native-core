//go:build amcore_aac

package codec

/*
#cgo CFLAGS: -I${SRCDIR}/../build/aac/src/common -I${SRCDIR}/../build/aac/src/decoder
#cgo LDFLAGS: ${SRCDIR}/../build/aac/libxaacdec.a -lm
#include "aac_bridge.h"
*/
import "C"
import (
	"errors"
	"fmt"
	"sync"
	"unsafe"
)

const HasAAC = true

type aacDecoder struct {
	mu      sync.Mutex
	ptr     unsafe.Pointer
	format  Format
	started bool
}

func NewAAC(config []byte) (PCMDecoder, error) {
	decoder, err := newAAC(config)
	if err != nil {
		return nil, err
	}
	return decoder, nil
}

func newAAC(config []byte) (*aacDecoder, error) {
	format, err := aacLCFormat(config)
	if err != nil {
		return nil, err
	}
	var reason C.int
	p := C.amcore_aac_open((*C.uint8_t)(unsafe.Pointer(&config[0])), C.uint32_t(len(config)), &reason)
	if p == nil {
		return nil, fmt.Errorf("AAC decoder init failed: %d", int(reason))
	}
	return &aacDecoder{ptr: p, format: format}, nil
}

// NewLiveAAC decodes the first ADTS payload to determine the true output
// format. A broadcast can signal SBR in-band while its ADTS header says AAC-LC.
func NewLiveAAC(config, firstPacket []byte) (PCMDecoder, []byte, error) {
	d, err := newAAC(config)
	if err != nil {
		return nil, nil, err
	}
	pcm, err := d.decode(firstPacket, true)
	if err != nil {
		d.Close()
		return nil, nil, err
	}
	return d, pcm, nil
}

func (d *aacDecoder) Format() Format { return d.format }

func (d *aacDecoder) Close() {
	d.mu.Lock()
	defer d.mu.Unlock()
	C.amcore_aac_close(d.ptr)
	d.ptr = nil
}

func (d *aacDecoder) Decode(packet []byte) ([]byte, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.decode(packet, false)
}

func (d *aacDecoder) decode(packet []byte, detectSBR bool) ([]byte, error) {
	if d.ptr == nil || len(packet) == 0 || len(packet) > 64<<10 {
		return nil, errors.New("invalid AAC decode request")
	}
	frameBytes := int(d.format.Channels) * 2
	capacity := int(d.format.FramesPerPacket) * frameBytes
	if detectSBR {
		capacity *= 2
	}
	pcm := make([]byte, capacity)
	var rate, channels C.uint32_t
	result := C.amcore_aac_decode(d.ptr, (*C.uint8_t)(unsafe.Pointer(&packet[0])), C.uint32_t(len(packet)), (*C.uint8_t)(unsafe.Pointer(&pcm[0])), C.uint32_t(len(pcm)), &rate, &channels)
	if result < 0 {
		return nil, fmt.Errorf("AAC decode failed (%d)", int(result))
	}
	if detectSBR && uint32(rate) == 2*d.format.SampleRate {
		d.format.SampleRate *= 2
		d.format.FramesPerPacket *= 2
	}
	if uint32(rate) != d.format.SampleRate || uint32(channels) != uint32(d.format.Channels) {
		return nil, errors.New("unexpected AAC output format")
	}
	pcm = pcm[:int(d.format.FramesPerPacket)*frameBytes]
	if int(result) != len(pcm) {
		// libxaac trims up to 5 ms of startup priming, including with its
		// limiter disabled. Preserve the container's first packet timestamp.
		priming := int(d.format.SampleRate/200) * frameBytes
		if d.started || result <= 0 || int(result) != len(pcm)-priming {
			return nil, fmt.Errorf("AAC decoder returned %d bytes; expected %d", int(result), len(pcm))
		}
		copy(pcm[priming:], pcm[:int(result)])
		clear(pcm[:priming])
	}
	d.started = true
	return pcm, nil
}
