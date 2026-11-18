package stream

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/Eyevinn/mp4ff/mp4"
	"github.com/InkCitrus/apple-music-native-core/catalog"
	"github.com/InkCitrus/apple-music-native-core/codec"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type syntheticKeys struct{ opened int }

func (k *syntheticKeys) OpenKey(id, uri string) error { k.opened++; return nil }
func (k *syntheticKeys) Decrypt(b []byte) error {
	for i := 0; i < len(b)&^15; i++ {
		b[i] ^= 0xaa
	}
	return nil
}
func syntheticMedia(t *testing.T) ([]byte, int, []byte) {
	t.Helper()
	data, e := os.ReadFile("../codec/testdata/pcm-24-96000.m4a")
	if e != nil {
		t.Fatal(e)
	}
	original, e := mp4.DecodeFile(bytes.NewReader(data))
	if e != nil {
		t.Fatal(e)
	}
	track := original.Moov.Traks[0]
	body := track.Mdia.Minf.Stbl.Stsd.Children[0].(*mp4.UnknownBox).Payload()
	var config []byte
	for p := 28; p+8 <= len(body); {
		n := int(binary.BigEndian.Uint32(body[p : p+4]))
		if n < 8 || p+n > len(body) {
			t.Fatal("invalid fixture")
		}
		if string(body[p+4:p+8]) == "alac" {
			config = body[p+8 : p+n]
			break
		}
		p += n
	}
	init := mp4.CreateEmptyInit()
	init.AddEmptyTrack(96000, "audio", "eng")
	entry := mp4.CreateAudioSampleEntryBox("enca", 2, 24, 48000, mp4.CreateUnknownBox("alac", uint64(8+len(config)), config))
	sinf := &mp4.SinfBox{}
	sinf.AddChild(&mp4.FrmaBox{DataFormat: "alac"})
	sinf.AddChild(&mp4.SchmBox{SchemeType: "cbcs", SchemeVersion: 65536})
	schi := &mp4.SchiBox{}
	schi.AddChild(&mp4.TencBox{Version: 1, DefaultIsProtected: 1, DefaultKID: make([]byte, 16), DefaultConstantIV: make([]byte, 16)})
	sinf.AddChild(schi)
	entry.AddChild(sinf)
	init.Moov.Traks[0].Mdia.Minf.Stbl.Stsd.AddChild(entry)
	fragment, _ := mp4.CreateFragment(1, 1)
	senc := mp4.CreateSencBox()
	var timestamp uint64
	for i := uint32(1); i <= track.GetNrSamples(); i++ {
		ranges, e := track.GetRangesForSampleInterval(i, i)
		if e != nil {
			t.Fatal(e)
		}
		r := ranges[0]
		packet := append([]byte(nil), data[r.Offset:r.Offset+r.Size]...)
		for j := 0; j < len(packet)&^15; j++ {
			packet[j] ^= 0xaa
		}
		dur := track.Mdia.Minf.Stbl.Stts.GetDur(i)
		fragment.AddFullSample(mp4.FullSample{Sample: mp4.NewSample(0, dur, uint32(len(packet)), 0), DecodeTime: timestamp, Data: packet})
		timestamp += uint64(dur)
		senc.AddSample(mp4.SencSample{})
	}
	fragment.Moof.Traf.AddChild(senc)
	var buffer bytes.Buffer
	if e = init.Encode(&buffer); e != nil {
		t.Fatal(e)
	}
	n := buffer.Len()
	if e = fragment.Encode(&buffer); e != nil {
		t.Fatal(e)
	}
	expected, _ := os.ReadFile("../codec/testdata/pcm-24-96000.pcm")
	return buffer.Bytes(), n, expected
}
func TestEncryptedHLSToPCMAndSeek(t *testing.T) {
	data, n, want := syntheticMedia(t)
	old := mediaClient
	mediaClient = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "" || r.Header.Get("Music-User-Token") != "" {
			t.Error("tokens sent to CDN")
		}
		a := strings.Split(strings.TrimPrefix(r.Header.Get("Range"), "bytes="), "-")
		start, _ := strconv.Atoi(a[0])
		end, _ := strconv.Atoi(a[1])
		return &http.Response{StatusCode: 206, Header: http.Header{"Content-Range": []string{fmt.Sprintf("bytes %d-%d/%d", start, end, len(data))}}, Body: io.NopCloser(bytes.NewReader(data[start : end+1]))}, nil
	})}
	defer func() { mediaClient = old }()
	p := catalog.Plan{TrackID: "123", MediaURL: "https://fixture.invalid/playlist", Playlist: fmt.Sprintf("#EXTM3U\n#EXT-X-MAP:URI=\"audio\",BYTERANGE=\"%d@0\"\n#EXT-X-KEY:METHOD=SAMPLE-AES,URI=\"skd://itunes.apple.com/test\"\n#EXTINF:1,\n#EXT-X-BYTERANGE:%d@%d\naudio\n#EXT-X-ENDLIST\n", n, len(data)-n, n)}
	for _, start := range []float64{0, 0.5} {
		var got []byte
		keys := &syntheticKeys{}
		e := Decode(context.Background(), p, start, keys, func(f codec.Format) error {
			if f.SampleRate != 96000 || f.Bits != 24 {
				t.Fatal(f)
			}
			return nil
		}, func(b []byte) error { got = append(got, b...); return nil })
		if e != nil {
			t.Fatal(e)
		}
		offset := int(start*96000) * 6
		if !bytes.Equal(got, want[offset:]) {
			t.Fatalf("PCM mismatch at seek %v: got %d want %d", start, len(got), len(want)-offset)
		}
		if keys.opened != 1 {
			t.Fatal("wrong license context count")
		}
	}
}

func TestPreparedStartOverlapsDownloadsAndReusesBytes(t *testing.T) {
	data, n, want := syntheticMedia(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	requests := make(chan string, 4)
	release := make(chan struct{})
	previousClient := mediaClient
	mediaClient = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		requests <- r.URL.Path
		select {
		case <-release:
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
		start, end := 0, n-1
		if r.URL.Path == "/first" {
			start, end = n, len(data)-1
		}
		return &http.Response{StatusCode: 206, Header: http.Header{"Content-Range": []string{fmt.Sprintf("bytes %d-%d/%d", start, end, len(data))}}, Body: io.NopCloser(bytes.NewReader(data[start : end+1]))}, nil
	})}
	defer func() { mediaClient = previousClient }()
	plan := catalog.Plan{TrackID: "123", MediaURL: "https://fixture.invalid/playlist", Playlist: fmt.Sprintf("#EXTM3U\n#EXT-X-MAP:URI=\"init\",BYTERANGE=\"%d@0\"\n#EXT-X-KEY:METHOD=SAMPLE-AES,URI=\"skd://license.invalid/test\"\n#EXTINF:1,\n#EXT-X-BYTERANGE:%d@%d\nfirst\n", n, len(data)-n, n)}
	if _, err := Prepare(ctx, plan, 0, int64(len(data)-1)); err == nil || len(requests) != 0 {
		t.Fatal("preparation exceeded its byte limit")
	}
	var input *Prepared
	var prepareErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		input, prepareErr = Prepare(ctx, plan, 0, int64(len(data)))
	}()
	defer func() { cancel(); <-done }()
	seen := map[string]bool{}
	for range 2 {
		select {
		case path := <-requests:
			seen[path] = true
		case <-ctx.Done():
			t.Fatal("startup requests did not overlap")
		}
	}
	if !seen["/init"] || !seen["/first"] {
		t.Fatal("wrong startup ranges", seen)
	}
	close(release)
	<-done
	if prepareErr != nil {
		t.Fatal(prepareErr)
	}
	var got []byte
	if err := input.Decode(ctx, &syntheticKeys{}, func(codec.Format) error { return nil }, func(pcm []byte) error { got = append(got, pcm...); return nil }); err != nil {
		t.Fatal(err)
	}
	if len(requests) != 0 || !bytes.Equal(got, want) {
		t.Fatal("prepared playback must reuse the startup bytes and preserve PCM")
	}
}
func TestInvalidRanges(t *testing.T) {
	for _, v := range []string{"1", "0@0", "33554433@0", "2@-1", "1@9223372036854775807"} {
		_, _, e := byteRange(v)
		if e == nil {
			t.Errorf("accepted %s", v)
		}
	}
}

func TestParallelDRMKeysDoNotReplaceFairPlay(t *testing.T) {
	fairplay := `#EXT-X-KEY:METHOD=SAMPLE-AES,URI="skd://license.invalid/first",KEYFORMAT="com.apple.streamingkeydelivery"`
	widevine := `#EXT-X-KEY:METHOD=SAMPLE-AES,URI="data:text/plain;base64,SYNTHETIC_WIDEVINE",KEYFORMAT="urn:uuid:edef8ba9-79d6-4ace-a3c8-27dcd51d21ed"`
	playready := `#EXT-X-KEY:METHOD=SAMPLE-AES,URI="data:text/plain;base64,SYNTHETIC_PLAYREADY",KEYFORMAT="com.microsoft.playready"`
	for _, declarations := range []string{
		fairplay + "\n" + playready + "\n" + widevine,
		widevine + "\n" + fairplay + "\n" + playready,
		playready + "\n" + widevine + "\n" + fairplay,
	} {
		p := catalog.Plan{MediaURL: "https://media.invalid/song.m3u8", Playlist: "#EXTM3U\n" +
			`#EXT-X-MAP:URI="audio",BYTERANGE="100@0"` + "\n" + declarations +
			"\n#EXTINF:6,\n#EXT-X-BYTERANGE:100@100\naudio\n" +
			playready + "\n#EXTINF:6,\n#EXT-X-BYTERANGE:100@200\naudio\n" +
			strings.Replace(fairplay, "first", "rotated", 1) + "\n" + widevine +
			"\n#EXTINF:6,\n#EXT-X-BYTERANGE:100@300\naudio\n#EXT-X-ENDLIST\n"}
		_, segments, err := playlist(p)
		if err != nil {
			t.Fatal(err)
		}
		if len(segments) != 3 || segments[0].Key != "skd://license.invalid/first" || segments[1].Key != segments[0].Key || segments[2].Key != "skd://license.invalid/rotated" {
			t.Fatal("independent DRM declarations or key rotation changed the FairPlay context")
		}
	}
}

func TestMissingNativeDRMKeyFailsBeforeFetching(t *testing.T) {
	p := catalog.Plan{MediaURL: "https://media.invalid/song.m3u8", Playlist: "#EXTM3U\n" +
		`#EXT-X-MAP:URI="audio",BYTERANGE="100@0"` + "\n" +
		`#EXT-X-KEY:METHOD=SAMPLE-AES,URI="data:text/plain;base64,SYNTHETIC",KEYFORMAT="com.microsoft.playready"` +
		"\n#EXTINF:6,\n#EXT-X-BYTERANGE:100@100\naudio\n"}
	if _, _, err := playlist(p); err == nil || err.Error() != "native FairPlay key unavailable" {
		t.Fatal("accepted a playlist without a supported native key")
	}
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type observedBody struct {
	io.Reader
	closed func()
}

func (b observedBody) Close() error {
	if b.closed != nil {
		b.closed()
	}
	return nil
}

func TestPrefetchOverlapsOutputAndKeepsOnlyOneSegmentAhead(t *testing.T) {
	data, n, want := syntheticMedia(t)
	duration := float64(len(want)) / 6 / 96000
	firstOutput, nextDownloaded, thirdRequested := make(chan struct{}), make(chan struct{}), make(chan struct{})
	release := make(chan struct{})
	unblock := func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	old := mediaClient
	mediaClient = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/third" {
			close(thirdRequested)
		}
		bounds := strings.Split(strings.TrimPrefix(r.Header.Get("Range"), "bytes="), "-")
		start, _ := strconv.Atoi(bounds[0])
		end, _ := strconv.Atoi(bounds[1])
		var closed func()
		if r.URL.Path == "/next" {
			closed = func() { close(nextDownloaded) }
		}
		return &http.Response{StatusCode: 206, Header: http.Header{"Content-Range": []string{fmt.Sprintf("bytes %d-%d/%d", start, end, len(data))}}, Body: observedBody{bytes.NewReader(data[start : end+1]), closed}}, nil
	})}
	p := catalog.Plan{TrackID: "123", MediaURL: "https://fixture.invalid/playlist", Playlist: fmt.Sprintf("#EXTM3U\n#EXT-X-MAP:URI=\"audio\",BYTERANGE=\"%d@0\"\n#EXT-X-KEY:METHOD=SAMPLE-AES,URI=\"skd://license.invalid/test\"\n", n)}
	for _, name := range []string{"first", "next", "third"} {
		p.Playlist += fmt.Sprintf("#EXTINF:%.8f,\n#EXT-X-BYTERANGE:%d@%d\n%s\n", duration, len(data)-n, n, name)
	}
	var got []byte
	var result error
	var first sync.Once
	done := make(chan struct{})
	go func() {
		defer close(done)
		result = Decode(ctx, p, 0, &syntheticKeys{}, func(codec.Format) error { return nil }, func(pcm []byte) error {
			first.Do(func() {
				close(firstOutput)
				select {
				case <-release:
				case <-ctx.Done():
				}
			})
			got = append(got, pcm...)
			return ctx.Err()
		})
	}()
	defer func() { cancel(); unblock(); <-done; mediaClient = old }()
	select {
	case <-firstOutput:
	case <-time.After(2 * time.Second):
		t.Fatal("first output did not start")
	}
	select {
	case <-nextDownloaded:
	case <-time.After(2 * time.Second):
		t.Fatal("next segment was not fetched while output was blocked")
	}
	select {
	case <-thirdRequested:
		t.Fatal("prefetch exceeded one segment ahead")
	case <-time.After(50 * time.Millisecond):
	}
	unblock()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("decode did not finish")
	}
	if result != nil {
		t.Fatal(result)
	}
	if !bytes.Equal(got, bytes.Repeat(want, 3)) {
		t.Fatal("prefetch changed PCM ordering or contents")
	}
}

func TestOutputFailureCancelsInFlightPrefetch(t *testing.T) {
	data, n, _ := syntheticMedia(t)
	started, canceled := make(chan struct{}), make(chan struct{})
	old := mediaClient
	mediaClient = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/next" {
			close(started)
			<-r.Context().Done()
			close(canceled)
			return nil, r.Context().Err()
		}
		bounds := strings.Split(strings.TrimPrefix(r.Header.Get("Range"), "bytes="), "-")
		start, _ := strconv.Atoi(bounds[0])
		end, _ := strconv.Atoi(bounds[1])
		return &http.Response{StatusCode: 206, Header: http.Header{"Content-Range": []string{fmt.Sprintf("bytes %d-%d/%d", start, end, len(data))}}, Body: io.NopCloser(bytes.NewReader(data[start : end+1]))}, nil
	})}
	defer func() { mediaClient = old }()
	p := catalog.Plan{TrackID: "123", MediaURL: "https://fixture.invalid/playlist", Playlist: fmt.Sprintf("#EXTM3U\n#EXT-X-MAP:URI=\"audio\",BYTERANGE=\"%d@0\"\n#EXT-X-KEY:METHOD=SAMPLE-AES,URI=\"skd://license.invalid/test\"\n#EXTINF:1,\n#EXT-X-BYTERANGE:%d@%d\nfirst\n#EXTINF:1,\n#EXT-X-BYTERANGE:%d@%d\nnext\n", n, len(data)-n, n, len(data)-n, n)}
	sinkError := errors.New("synthetic output failure")
	err := Decode(context.Background(), p, 0, &syntheticKeys{}, func(codec.Format) error { return nil }, func([]byte) error {
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			return errors.New("prefetch did not start")
		}
		return sinkError
	})
	if !errors.Is(err, sinkError) {
		t.Fatal(err)
	}
	select {
	case <-canceled:
	default:
		t.Fatal("decode returned with a live prefetch request")
	}
}
