package codec

/*
#cgo CXXFLAGS: -std=c++17 -O2
#cgo darwin LDFLAGS: -lc++
#cgo linux windows LDFLAGS: -lstdc++
#include "bridge.h"
*/
import "C"
import (
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"unsafe"
)

type Format struct {
	Codec           string `json:"codec"`
	SampleRate      uint32 `json:"sampleRate"`
	Channels        uint8  `json:"channels"`
	Bits            uint8  `json:"bits"`
	FramesPerPacket uint32 `json:"framesPerPacket"`
}

type PCMDecoder interface {
	Format() Format
	Decode([]byte) ([]byte, error)
	Close()
}
type Decoder struct {
	mu     sync.Mutex
	ptr    unsafe.Pointer
	format Format
}

func New(cookie []byte) (*Decoder, error) {
	if len(cookie) != 24 {
		return nil, errors.New("ALAC config must contain 24 bytes")
	}
	f := Format{Codec: "alac", SampleRate: binary.BigEndian.Uint32(cookie[20:24]), Channels: cookie[9], Bits: cookie[5], FramesPerPacket: binary.BigEndian.Uint32(cookie[:4])}
	if f.SampleRate < 8000 || f.SampleRate > 384000 || f.Channels == 0 || f.Channels > 8 || f.FramesPerPacket == 0 || f.FramesPerPacket > 65536 || (f.Bits != 16 && f.Bits != 24 && f.Bits != 32) {
		return nil, errors.New("unsupported ALAC config")
	}
	p := C.alac_decoder_create(unsafe.Pointer(&cookie[0]), C.uint32_t(len(cookie)))
	if p == nil {
		return nil, errors.New("ALAC decoder init failed")
	}
	return &Decoder{ptr: p, format: f}, nil
}
func (d *Decoder) Format() Format { return d.format }
func (d *Decoder) Close() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.ptr != nil {
		C.alac_decoder_close(d.ptr)
		d.ptr = nil
	}
}
func (d *Decoder) Decode(packet []byte) ([]byte, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.ptr == nil {
		return nil, errors.New("ALAC decoder closed")
	}
	if len(packet) == 0 || len(packet) > 4<<20 {
		return nil, errors.New("invalid ALAC packet size")
	}
	// The reference decoder reads word-sized fields; keep a small zero tail.
	padded := make([]byte, len(packet)+32)
	copy(padded, packet)
	capacity := int(d.format.FramesPerPacket) * int(d.format.Channels) * int(d.format.Bits/8)
	pcm := make([]byte, capacity)
	var frames C.uint32_t
	code := C.alac_decoder_decode(d.ptr, unsafe.Pointer(&padded[0]), C.uint32_t(len(packet)), unsafe.Pointer(&pcm[0]), C.uint32_t(capacity), &frames)
	if code != 0 {
		return nil, fmt.Errorf("ALAC decode failed: %d", int(code))
	}
	if uint32(frames) > d.format.FramesPerPacket {
		return nil, errors.New("ALAC returned too many frames")
	}
	return pcm[:int(frames)*int(d.format.Channels)*int(d.format.Bits/8)], nil
}
