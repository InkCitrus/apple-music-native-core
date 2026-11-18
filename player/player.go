// Package player provides a reusable, headless Apple Music player.
package player

import (
	"context"
	"errors"
	"github.com/InkCitrus/apple-music-native-core/audio"
	"github.com/InkCitrus/apple-music-native-core/catalog"
	"github.com/InkCitrus/apple-music-native-core/codec"
	"github.com/InkCitrus/apple-music-native-core/native"
	"github.com/InkCitrus/apple-music-native-core/stream"
	"math"
	"regexp"
	"sync"
	"time"
)

type State struct {
	Status      string        `json:"status"`
	TrackID     string        `json:"trackId,omitempty"`
	Position    int64         `json:"position"` // milliseconds
	Duration    int64         `json:"duration"`
	Volume      float64       `json:"volume"`           // 0..100
	Format      *codec.Format `json:"format,omitempty"` // source, not a DAC claim
	OutputRate  float64       `json:"outputRate,omitempty"`
	Error       string        `json:"error,omitempty"`
	OutputStats *audio.Stats  `json:"outputStats,omitempty"`
}
type Output interface {
	Write([]byte) error
	Pause(bool) error
	Volume(float32) error
	Position() float64
	Abort()
	Close() float64
}
type Keys interface {
	stream.Keys
	Close()
	Abort()
}
type Resolver interface {
	Resolve(context.Context, string, string) (catalog.Plan, error)
}
type Config struct {
	Native     native.Config
	Resolver   Resolver                                    // nil uses the profile's native session
	OpenKeys   func() (Keys, error)                        // nil starts the SDK helper
	OpenOutput func(codec.Format, float32) (Output, error) // nil uses the system output
}
type job struct {
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
	sink   Output
	offset int64
	frames int64
	nextID string
}
type preparedTrack struct {
	id, quality string
	account     string
	until       time.Time
	cancel      context.CancelFunc
	ready       chan struct{}
	duration    int64
	input       *stream.Prepared
}
type Player struct {
	op     sync.Mutex
	mu     sync.Mutex
	config Config
	state  State
	job    *job
	ahead  *preparedTrack
	keys   Keys
	closed bool
}

var catalogID = regexp.MustCompile(`^[0-9]{1,24}$`)

func accountOf(resolver Resolver) string {
	if client, ok := resolver.(*catalog.Client); ok {
		return client.Tokens.User
	}
	return ""
}

func New(c Config) *Player { return &Player{config: c, state: State{Status: "idle", Volume: 100}} }
func (p *Player) State() State {
	p.mu.Lock()
	defer p.mu.Unlock()
	s := p.state
	if p.job != nil && p.job.sink != nil {
		if output, ok := p.job.sink.(interface{ Stats() audio.Stats }); ok {
			stats := output.Stats()
			s.OutputStats = &stats
		}
		s.Position = p.job.offset + int64(math.Max(0, p.job.sink.Position())*1000)
		if s.Duration > 0 && s.Position > s.Duration {
			s.Position = s.Duration
		}
	}
	return s
}

// stopLocked is called with op held; it cancels I/O, discards queued PCM, and
// waits for the old decoder before a new track can use the native key context.
func (p *Player) stopLocked() {
	p.mu.Lock()
	j := p.job
	if j != nil {
		j.cancel()
		if j.sink != nil {
			j.sink.Abort()
		}
	}
	p.mu.Unlock()
	if j != nil {
		select {
		case <-j.done:
		case <-time.After(time.Second):
			p.mu.Lock()
			keys := p.keys
			p.keys = nil
			p.mu.Unlock()
			if keys != nil {
				keys.Abort()
			}
			<-j.done
		}
	}
	p.mu.Lock()
	if p.job == j {
		p.job = nil
	}
	p.mu.Unlock()
}
func (p *Player) Play(ctx context.Context, id string, position int64, quality string) error {
	return p.PlayWithNext(ctx, id, position, quality, "")
}

func (p *Player) PlayWithNext(ctx context.Context, id string, position int64, quality, nextID string) error {
	if !catalogID.MatchString(id) {
		return errors.New("unsupported")
	}
	if position < 0 {
		return errors.New("invalid position")
	}
	if quality != "hires" && quality != "lossless" {
		return errors.New("unsupported quality")
	}
	if nextID == id || !catalogID.MatchString(nextID) {
		nextID = ""
	}
	p.op.Lock()
	defer p.op.Unlock()
	p.mu.Lock()
	closed := p.closed
	p.mu.Unlock()
	if closed {
		return errors.New("closed")
	}
	resolver := p.config.Resolver
	if resolver == nil {
		if _, e := catalog.LoadTokens(p.config.Native.Profile); e != nil {
			return e
		}
	}
	p.stopLocked()
	child, cancel := context.WithCancel(ctx)
	j := &job{ctx: child, cancel: cancel, done: make(chan struct{}), offset: position, nextID: nextID}
	p.mu.Lock()
	var ahead *preparedTrack
	if a := p.ahead; a != nil {
		if a.quality != quality || time.Now().After(a.until) {
			p.clearAheadLocked()
		} else if a.id == id && position == 0 {
			ahead, p.ahead = a, nil
		} else if a.id != nextID {
			p.clearAheadLocked()
		}
	}
	p.job = j
	p.state = State{Status: "buffering", TrackID: id, Position: position, Volume: p.state.Volume}
	p.mu.Unlock()
	go p.run(j, resolver, id, quality, ahead)
	return nil
}

func (p *Player) clearAheadLocked() {
	if p.ahead != nil {
		p.ahead.cancel()
		p.ahead = nil
	}
}

func (p *Player) prepareNext(j *job, resolver Resolver, quality string, nearEnd bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if j.nextID == "" || p.job != j || j.ctx.Err() != nil {
		return
	}
	account := accountOf(resolver)
	if a := p.ahead; a != nil && a.account == account && time.Until(a.until) > 15*time.Second {
		select {
		case <-a.ready:
			if a.input != nil || !nearEnd {
				return
			}
		default:
			return
		}
	}
	p.clearAheadLocked()
	ctx, cancel := context.WithTimeout(context.WithoutCancel(j.ctx), 10*time.Second)
	a := &preparedTrack{id: j.nextID, quality: quality, account: account, until: time.Now().Add(45 * time.Second), cancel: cancel, ready: make(chan struct{})}
	p.ahead = a
	go func() {
		defer close(a.ready)
		defer cancel()
		plan, err := resolver.Resolve(ctx, a.id, a.quality)
		if err == nil && ctx.Err() == nil {
			a.duration = plan.DurationMs
			a.input, _ = stream.Prepare(ctx, plan, 0, 8<<20)
		}
	}()
}

func (p *Player) run(j *job, resolver Resolver, id, quality string, ahead *preparedTrack) {
	defer close(j.done)
	var keys Keys
	var err error
	// Resuming the native SDK refreshes cached API tokens before catalog access.
	if p.config.Resolver == nil {
		keys, err = p.openKeys(j.ctx)
		if err == nil {
			var tokens catalog.Tokens
			tokens, err = catalog.LoadTokens(p.config.Native.Profile)
			if err == nil {
				resolver = catalog.New(tokens)
			}
		}
	}
	var input *stream.Prepared
	var duration int64
	if err == nil && ahead != nil && ahead.account == accountOf(resolver) {
		select {
		case <-ahead.ready:
			if time.Now().Before(ahead.until) {
				input, duration = ahead.input, ahead.duration
			}
		default:
		}
	}
	if ahead != nil {
		ahead.cancel()
	}
	if err == nil && input == nil && j.ctx.Err() == nil {
		var plan catalog.Plan
		plan, err = resolver.Resolve(j.ctx, id, quality)
		if err == nil {
			duration = plan.DurationMs
			input, err = stream.Prepare(j.ctx, plan, float64(j.offset)/1000, 0)
		}
	}
	if err == nil && keys == nil && j.ctx.Err() == nil {
		keys, err = p.openKeys(j.ctx)
	}
	var format codec.Format
	if err == nil && j.ctx.Err() == nil {
		p.mu.Lock()
		p.state.Duration = duration
		p.mu.Unlock()
		startedAhead, checkedEnd := false, false
		err = input.Decode(j.ctx, keys, func(f codec.Format) error {
			format = f
			p.mu.Lock()
			defer p.mu.Unlock()
			if j.ctx.Err() != nil {
				return j.ctx.Err()
			}
			var e error
			if p.config.OpenOutput != nil {
				j.sink, e = p.config.OpenOutput(f, float32(p.state.Volume/100))
			} else {
				j.sink, e = audio.New(f.SampleRate, f.Channels, f.Bits, float32(p.state.Volume/100))
			}
			if e != nil {
				return e
			}
			p.state.Format = &f
			p.state.OutputRate = audio.HardwareRate()
			if p.state.Status == "paused" {
				return j.sink.Pause(true)
			}
			return nil
		}, func(pcm []byte) error {
			if err := j.ctx.Err(); err != nil {
				return err
			}
			if err := j.sink.Write(pcm); err != nil {
				return err
			}
			j.frames += int64(len(pcm)) / (int64(format.Channels) * int64(format.Bits/8))
			p.mu.Lock()
			if p.state.Status == "buffering" {
				p.state.Status = "playing"
			}
			p.mu.Unlock()
			if j.nextID != "" && j.frames >= int64(format.SampleRate)/2 {
				nearEnd := duration > 0 && j.offset+j.frames*1000/int64(format.SampleRate) >= duration-10000
				if !startedAhead || nearEnd && !checkedEnd {
					p.prepareNext(j, resolver, quality, nearEnd)
					startedAhead = true
					checkedEnd = nearEnd
				}
			}
			return nil
		})
	}
	if err == nil && j.sink != nil {
		if starter, ok := j.sink.(interface{ Start() error }); ok {
			err = starter.Start()
		}
	}
	if err == nil && j.sink != nil {
		// Drain under normal playback controls; Pause/Seek/Stop remain responsive.
		for j.ctx.Err() == nil && j.sink.Position()+0.002 < float64(j.frames)/float64(format.SampleRate) {
			select {
			case <-j.ctx.Done():
			case <-time.After(10 * time.Millisecond):
			}
		}
	}
	p.mu.Lock()
	if j.sink != nil {
		sink := j.sink
		if output, ok := sink.(interface{ Stats() audio.Stats }); ok {
			stats := output.Stats()
			p.state.OutputStats = &stats
		}
		j.sink = nil
		if j.ctx.Err() != nil || err != nil {
			sink.Abort()
		}
		sink.Close()
	}
	if j.ctx.Err() == nil {
		if err != nil {
			p.clearAheadLocked()
			p.state.Status = "error"
			p.state.Error = classify(err)
			if p.keys != nil {
				p.keys.Close()
				p.keys = nil
			}
		} else {
			p.state.Status = "ended"
			p.state.Position = p.state.Duration
		}
	}
	p.mu.Unlock()
}
func classify(err error) string {
	if err.Error() == "signIn" {
		return "signIn"
	}
	return "unavailable"
}
func (p *Player) Pause(paused bool) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.job == nil || p.state.Status == "ended" || p.state.Status == "error" {
		return nil
	}
	if p.job.sink != nil {
		if err := p.job.sink.Pause(paused); err != nil {
			return err
		}
	}
	if paused {
		p.state.Status = "paused"
	} else {
		p.state.Status = "playing"
	}
	return nil
}
func (p *Player) Volume(percent float64) error {
	if math.IsNaN(percent) || math.IsInf(percent, 0) || percent < 0 || percent > 100 {
		return errors.New("invalid volume")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.job != nil && p.job.sink != nil {
		if err := p.job.sink.Volume(float32(percent / 100)); err != nil {
			return err
		}
	}
	p.state.Volume = percent
	return nil
}
func (p *Player) Seek(ctx context.Context, ms int64, quality string) error {
	s := p.State()
	if s.TrackID == "" {
		return nil
	}
	if s.Duration > 0 && ms >= s.Duration {
		return errors.New("invalid position")
	}
	p.mu.Lock()
	nextID := ""
	if p.job != nil && p.state.TrackID == s.TrackID {
		nextID = p.job.nextID
	}
	p.mu.Unlock()
	err := p.PlayWithNext(ctx, s.TrackID, ms, quality, nextID)
	if err == nil && s.Status == "paused" {
		err = p.Pause(true)
	}
	return err
}
func (p *Player) Stop() {
	p.op.Lock()
	defer p.op.Unlock()
	p.stopLocked()
	p.mu.Lock()
	p.clearAheadLocked()
	p.state = State{Status: "idle", Volume: p.state.Volume}
	p.mu.Unlock()
}
func (p *Player) Release() { p.release(false) }
func (p *Player) Close()   { p.release(true) }
func (p *Player) release(closed bool) {
	p.op.Lock()
	defer p.op.Unlock()
	p.stopLocked()
	p.mu.Lock()
	p.clearAheadLocked()
	p.closed = closed
	p.state = State{Status: "idle", Volume: p.state.Volume}
	keys := p.keys
	p.keys = nil
	p.mu.Unlock()
	if keys != nil {
		keys.Close()
	}
}

func (p *Player) openKeys(ctx context.Context) (Keys, error) {
	p.mu.Lock()
	keys := p.keys
	p.mu.Unlock()
	var err error
	if keys == nil && ctx.Err() == nil {
		if p.config.OpenKeys != nil {
			keys, err = p.config.OpenKeys()
		} else {
			initCtx, cancel := context.WithCancel(context.Background())
			started := make(chan struct{})
			go func() {
				select {
				case <-ctx.Done():
					cancel()
				case <-started:
				}
			}()
			keys, err = native.StartContext(initCtx, p.config.Native)
			close(started)
			if err != nil {
				cancel()
			}
		}
		if err == nil {
			p.mu.Lock()
			p.keys = keys
			p.mu.Unlock()
		}
	}
	return keys, err
}
