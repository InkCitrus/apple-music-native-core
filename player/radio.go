package player

import (
	"context"
	"errors"

	"github.com/InkCitrus/apple-music-native-core/codec"
	"github.com/InkCitrus/apple-music-native-core/stream"
)

// PrepareRadio checks that the station has a decodable live segment before a
// client replaces its queue. It leaves the current playback job alone.
func (p *Player) PrepareRadio(ctx context.Context, id string) error {
	_, err := p.prepareRadio(ctx, id)
	return err
}

func (p *Player) prepareRadio(ctx context.Context, id string) (*stream.PreparedRadio, error) {
	p.mu.Lock()
	prepared, closed := p.radio, p.closed
	p.mu.Unlock()
	if closed {
		return nil, errors.New("closed")
	}
	if prepared.ReadyFor(id) {
		return prepared, nil
	}
	prepared, err := stream.PrepareRadio(ctx, id)
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil, errors.New("closed")
	}
	p.radio = prepared
	return prepared, nil
}

// PlayRadio starts a public live station by Apple Music station ID. Playback
// uses the system audio sink and is independent of the song quality setting.
func (p *Player) PlayRadio(ctx context.Context, id string) error {
	p.op.Lock()
	p.playRequest++
	request := p.playRequest
	p.op.Unlock()
	prepared, err := p.prepareRadio(ctx, id)
	if err != nil {
		return err
	}
	p.op.Lock()
	defer p.op.Unlock()
	if request != p.playRequest {
		return errors.New("playback request superseded")
	}
	p.mu.Lock()
	closed := p.closed
	p.mu.Unlock()
	if closed {
		return errors.New("closed")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	p.stopLocked()
	child, cancel := context.WithCancel(ctx)
	j := &job{ctx: child, cancel: cancel, done: make(chan struct{})}
	p.mu.Lock()
	p.clearAheadLocked()
	if p.radio == prepared {
		p.radio = nil
	}
	p.job = j
	p.state = State{Status: "buffering", TrackID: id, Live: true, Volume: p.state.Volume}
	p.mu.Unlock()
	go func() {
		defer close(j.done)
		var format codec.Format
		err := prepared.Decode(j.ctx, func(f codec.Format) error {
			format = f
			return p.openOutput(j, f)
		}, func(pcm []byte) error {
			return p.writeOutput(j, format, pcm)
		})
		p.finishJob(j, err)
	}()
	return nil
}
