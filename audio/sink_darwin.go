package audio

/*
#cgo CXXFLAGS: -std=c++17 -O2
#cgo LDFLAGS: -framework AudioToolbox -framework CoreAudio -lc++
#include "queue.h"
*/
import "C"
import (
	"errors"
	"fmt"
	"unsafe"
)

type Sink struct{ ptr unsafe.Pointer }

func New(rate uint32, channels, bits uint8, volume float32) (*Sink, error) {
	p := C.pcm_queue_new(C.uint32_t(rate), C.uint32_t(channels), C.uint32_t(bits), C.float(volume))
	if p == nil {
		return nil, errors.New("CoreAudio PCM queue unavailable")
	}
	return &Sink{p}, nil
}
func (s *Sink) Write(data []byte) error {
	if len(data) == 0 {
		return nil
	}
	if r := C.pcm_queue_write(s.ptr, unsafe.Pointer(&data[0]), C.uint32_t(len(data))); r != 0 {
		return fmt.Errorf("PCM output failed (%d)", int(r))
	}
	return nil
}
func (s *Sink) Pause(pause bool) error {
	p := 0
	if pause {
		p = 1
	}
	if r := C.pcm_queue_pause(s.ptr, C.int(p)); r != 0 {
		return fmt.Errorf("PCM pause/resume failed (%d)", int(r))
	}
	return nil
}
func (s *Sink) Volume(volume float32) error {
	if r := C.pcm_queue_volume(s.ptr, C.float(volume)); r != 0 {
		return fmt.Errorf("PCM volume failed (%d)", int(r))
	}
	return nil
}
func (s *Sink) Position() float64      { return float64(C.pcm_queue_position(s.ptr)) }
func (s *Sink) CurrentVolume() float64 { return float64(C.pcm_queue_get_volume(s.ptr)) }
func (s *Sink) Start() error {
	if C.pcm_queue_start(s.ptr) != 0 {
		return errors.New("audio start failed")
	}
	return nil
}
func (s *Sink) Abort() { C.pcm_queue_abort(s.ptr) }
func (s *Sink) Close() float64 {
	if s.ptr != nil {
		seconds := float64(C.pcm_queue_close(s.ptr))
		s.ptr = nil
		return seconds
	}
	return 0
}
func HardwareRate() float64 { return float64(C.pcm_queue_hardware_rate()) }
