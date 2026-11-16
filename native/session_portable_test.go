//go:build linux || windows || mygo_portable_test

package native

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestPersistentNativePrivatePipe(t *testing.T) {
	helper := os.Getenv("AMCORE_MOCK_HELPER")
	runtime := os.Getenv("AMCORE_MOCK_RUNTIME")
	if helper == "" {
		t.Skip("run python3 scripts/test_native.py for the compiled SDK fixture")
	}
	profile := t.TempDir()
	os.WriteFile(filepath.Join(profile, "MUSIC_TOKEN"), []byte("SYNTHETIC"), 0600)
	c := Config{Helper: helper, Runtime: runtime, Profile: profile}
	s, e := Start(c)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	for _, id := range []string{"0", "123", "456"} {
		if e = s.OpenKey(id, "skd://itunes.apple.com/test"); e != nil {
			t.Fatal(e)
		}
		for _, n := range []int{16, 65536, 524288} {
			data := bytes.Repeat([]byte{0x55}, n)
			if e = s.Decrypt(data); e != nil {
				t.Fatal(e)
			}
			if !bytes.Equal(data, bytes.Repeat([]byte{0xff}, n)) {
				t.Fatal("sample differs")
			}
		}
	}
	var done sync.WaitGroup
	for n := 0; n < 3; n++ {
		done.Add(1)
		go func() { defer done.Done(); s.Abort() }()
	}
	done.Wait()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if s, e := StartContext(ctx, c); e == nil {
		s.Close()
		t.Fatal("started a cancelled native process")
	}
}
