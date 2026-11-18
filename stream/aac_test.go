//go:build amcore_aac

package stream

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Eyevinn/mp4ff/mp4"
	"github.com/InkCitrus/apple-music-native-core/catalog"
	"github.com/InkCitrus/apple-music-native-core/codec"
)

func TestLiveAACContinuesPastInitialWindow(t *testing.T) {
	data, err := os.ReadFile("../codec/testdata/aac-lc-stereo.m4a")
	if err != nil {
		t.Fatal(err)
	}
	file, err := mp4.DecodeFile(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	track := file.Moov.Traks[0]
	id3 := []byte{'I', 'D', '3', 4, 0, 0, 0, 0, 0, 1, 0}
	audio := append([]byte(nil), id3...)
	for i := uint32(1); i <= track.GetNrSamples(); i++ {
		ranges, err := track.GetRangesForSampleInterval(i, i)
		if err != nil || len(ranges) != 1 {
			t.Fatal("missing synthetic AAC packet", i, err)
		}
		r := ranges[0]
		size := int(r.Size) + 7
		// MPEG-4 AAC-LC, 44.1 kHz, stereo, 7-byte ADTS header.
		audio = append(audio, 0xff, 0xf1, 0x50, 0x80|byte(size>>11), byte(size>>3), byte(size<<5)|0x1f, 0xfc)
		audio = append(audio, data[r.Offset:r.Offset+r.Size]...)
		if i == 1 {
			audio = append(audio, id3...)
		}
	}
	var lists int
	var downloaded []string
	client := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			return nil, errors.New("broadcast repeated subscriber credentials")
		}
		var body []byte
		switch r.URL.Path {
		case "/Tune.ashx":
			if r.URL.Query().Get("id") != "s345732" {
				return nil, errors.New("wrong TuneIn station")
			}
			body = []byte(`{"body":[{"url":"https://itsliveradio.apple.com/master"}]}`)
		case "/master":
			body = []byte("#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=256000,CODECS=\"mp4a.40.2\"\nmedia\n")
		case "/media":
			lists++
			first := min(23, 20+lists)
			body = fmt.Appendf(nil, "#EXTM3U\n#EXT-X-MEDIA-SEQUENCE:%d\n#EXT-X-TARGETDURATION:1\n#EXTINF:1,\n%d.aac\n#EXTINF:1,\n%d.aac\n", first, first, first+1)
		default:
			downloaded = append(downloaded, r.URL.Path)
			body = audio
		}
		return &http.Response{StatusCode: 200, Request: r, Body: io.NopCloser(bytes.NewReader(body))}, nil
	})}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	prepared, err := prepareRadio(ctx, client, "ra.978194965")
	if err != nil {
		t.Fatal(err)
	}
	total, formats, want := 0, 0, 3*int(track.GetNrSamples())*1024*4
	stop := errors.New("third segment received")
	err = prepared.Decode(ctx, func(f codec.Format) error {
		formats++
		if f.Codec != "aac" || f.SampleRate != 44100 || f.Channels != 2 {
			t.Errorf("unexpected live format: %+v", f)
		}
		return nil
	}, func(pcm []byte) error {
		total += len(pcm)
		if total >= want {
			return stop
		}
		return nil
	})
	if !errors.Is(err, stop) || formats != 1 || total != want {
		t.Fatalf("live decode got %d bytes in %d formats: %v", total, formats, err)
	}
	if len(downloaded) < 3 || strings.Join(downloaded[:3], ",") != "/21.aac,/22.aac,/23.aac" {
		t.Fatalf("did not follow live sequence: %v", downloaded)
	}
}

func TestAACNativeSamplePipeline(t *testing.T) {
	data, err := os.ReadFile("../codec/testdata/aac-lc-stereo.m4a")
	if err != nil {
		t.Fatal(err)
	}
	file, err := mp4.DecodeFile(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	tr := file.Moov.Traks[0]
	esds := tr.Mdia.Minf.Stbl.Stsd.Children[0].(*mp4.AudioSampleEntryBox).Esds
	init := mp4.CreateEmptyInit()
	init.AddEmptyTrack(44100, "audio", "eng")
	entry := mp4.CreateAudioSampleEntryBox("enca", 2, 16, 44100, esds)
	sinf := &mp4.SinfBox{}
	sinf.AddChild(&mp4.FrmaBox{DataFormat: "mp4a"})
	sinf.AddChild(&mp4.SchmBox{SchemeType: "cbcs", SchemeVersion: 65536})
	schi := &mp4.SchiBox{}
	schi.AddChild(&mp4.TencBox{Version: 1, DefaultIsProtected: 1, DefaultKID: make([]byte, 16), DefaultConstantIV: make([]byte, 16)})
	sinf.AddChild(schi)
	entry.AddChild(sinf)
	init.Moov.Traks[0].Mdia.Minf.Stbl.Stsd.AddChild(entry)
	fragment, err := mp4.CreateFragment(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	senc := mp4.CreateSencBox()
	var duration uint64
	for i := uint32(1); i <= tr.GetNrSamples(); i++ {
		ranges, err := tr.GetRangesForSampleInterval(i, i)
		if err != nil || len(ranges) != 1 {
			t.Fatal("invalid test packet", i, err)
		}
		r := ranges[0]
		packet := append([]byte(nil), data[r.Offset:r.Offset+r.Size]...)
		for i := 0; i < len(packet)&^15; i++ {
			packet[i] ^= 0xaa
		}
		frames := tr.Mdia.Minf.Stbl.Stts.GetDur(i)
		fragment.AddFullSample(mp4.FullSample{Sample: mp4.NewSample(0, frames, uint32(len(packet)), 0), DecodeTime: duration, Data: packet})
		duration += uint64(frames)
		senc.AddSample(mp4.SencSample{})
	}
	fragment.Moof.Traf.AddChild(senc)
	var container bytes.Buffer
	if err := init.Encode(&container); err != nil {
		t.Fatal(err)
	}
	mapBytes := container.Len()
	if err := fragment.Encode(&container); err != nil {
		t.Fatal(err)
	}
	media := container.Bytes()
	previous := mediaClient
	defer func() { mediaClient = previous }()
	mediaClient = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		var begin, end int
		if _, e := fmt.Sscanf(r.Header.Get("Range"), "bytes=%d-%d", &begin, &end); e != nil || begin < 0 || end < begin || end >= len(media) {
			return nil, fmt.Errorf("unexpected fixture range")
		}
		return &http.Response{StatusCode: 206, Header: http.Header{"Content-Range": []string{fmt.Sprintf("bytes %d-%d/%d", begin, end, len(media))}}, Body: io.NopCloser(bytes.NewReader(media[begin : end+1]))}, nil
	})}
	plan := catalog.Plan{TrackID: "123", MediaURL: "https://fixture.invalid/playlist", Playlist: fmt.Sprintf("#EXTM3U\n#EXT-X-MAP:URI=\"audio\",BYTERANGE=\"%d@0\"\n#EXT-X-KEY:METHOD=SAMPLE-AES,URI=\"skd://fixture.invalid/test\"\n#EXTINF:%.9f,\n#EXT-X-BYTERANGE:%d@%d\naudio\n#EXT-X-ENDLIST\n", mapBytes, float64(duration)/44100, len(media)-mapBytes, mapBytes)}
	var full []byte
	for _, start := range []float64{0, 0.1} {
		var pcm []byte
		keys := &syntheticKeys{}
		if err := Decode(context.Background(), plan, start, keys, func(f codec.Format) error {
			if f.Codec != "aac" || f.SampleRate != 44100 || f.Channels != 2 || f.Bits != 16 {
				t.Fatalf("wrong AAC format: %+v", f)
			}
			return nil
		}, func(frame []byte) error { pcm = append(pcm, frame...); return nil }); err != nil {
			t.Fatal(err)
		}
		if keys.opened != 1 {
			t.Fatal("wrong playback key context count")
		}
		if start == 0 {
			full = pcm
			if len(pcm) != int(duration)*4 {
				t.Fatalf("wrong AAC duration: %d frames, want %d", len(pcm)/4, duration)
			}
			var audible bool
			for i := 2 * 4096; i+4 <= len(pcm); i += 4 {
				l, r := int16(binary.LittleEndian.Uint16(pcm[i:])), int16(binary.LittleEndian.Uint16(pcm[i+2:]))
				audible = audible || l > 1000 && r < -1000
			}
			if !audible {
				t.Fatal("AAC fixture did not yield distinct audio in both channels")
			}
		} else if !bytes.Equal(pcm, full[4410*4:]) {
			t.Fatal("AAC seek did not reproduce the expected PCM suffix")
		}
	}
}
