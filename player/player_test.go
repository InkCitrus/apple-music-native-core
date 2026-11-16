package player

import (
	"context"
	"github.com/InkCitrus/apple-music-native-core/catalog"
	"sync"
	"testing"
	"time"
)

type blockedResolver struct {
	started chan struct{}
	once    sync.Once
}

func (r *blockedResolver) Resolve(ctx context.Context, id, q string) (catalog.Plan, error) {
	r.once.Do(func() { close(r.started) })
	<-ctx.Done()
	return catalog.Plan{}, ctx.Err()
}
func TestSwitchCancelAndPauseDuringBuffering(t *testing.T) {
	r := &blockedResolver{started: make(chan struct{})}
	p := New(Config{Resolver: r})
	defer p.Close()
	ctx := context.Background()
	if e := p.Play(ctx, "123", 0, "hires"); e != nil {
		t.Fatal(e)
	}
	<-r.started
	if e := p.Pause(true); e != nil {
		t.Fatal(e)
	}
	if s := p.State(); s.Status != "paused" {
		t.Fatal(s)
	}
	if e := p.Volume(0); e != nil {
		t.Fatal(e)
	}
	if e := p.Seek(ctx, 90000, "hires"); e != nil {
		t.Fatal(e)
	}
	if s := p.State(); s.Position != 90000 || s.Status != "paused" || s.Volume != 0 {
		t.Fatal(s)
	}
	done := make(chan struct{})
	go func() { p.Stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("stop blocked")
	}
	if p.State().Status != "idle" {
		t.Fatal(p.State())
	}
	p.Release()
	if e := p.Play(ctx, "456", 0, "lossless"); e != nil {
		t.Fatal("release prevents reuse", e)
	}
	p.Close()
	if e := p.Play(ctx, "789", 0, "hires"); e == nil {
		t.Fatal("closed player accepted work")
	}
}
func TestRejectNonfiniteVolume(t *testing.T) {
	p := New(Config{})
	defer p.Close()
	for _, v := range []float64{-1, 101} {
		if p.Volume(v) == nil {
			t.Fatal("invalid volume accepted")
		}
	}
}
