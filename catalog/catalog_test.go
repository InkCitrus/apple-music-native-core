package catalog

import (
	"strings"
	"testing"
)

const master = `#EXTM3U
#EXT-X-STREAM-INF:BANDWIDTH=256000,CODECS="mp4a.40.2"
aac.m3u8
#EXT-X-STREAM-INF:AVERAGE-BANDWIDTH=3000000,CODECS="alac",AUDIO="audio-alac-stereo-96000-24"
hires.m3u8
#EXT-X-STREAM-INF:AVERAGE-BANDWIDTH=1000000,CODECS="alac",AUDIO="audio-alac-stereo-44100-16"
cd.m3u8
#EXT-X-STREAM-INF:AVERAGE-BANDWIDTH=2000000,CODECS="alac",AUDIO="audio-alac-stereo-48000-24"
lossless.m3u8
`

func TestChooseALACQuality(t *testing.T) {
	for _, tc := range []struct{ quality, want string }{{"hires", "hires.m3u8"}, {"lossless", "lossless.m3u8"}} {
		got, e := SelectALAC("https://example.com/master.m3u8", master, tc.quality)
		if e != nil || !strings.HasSuffix(got, tc.want) {
			t.Fatalf("%s: %s, %v", tc.quality, got, e)
		}
	}
}
func TestNeverFallbackToAAC(t *testing.T) {
	if _, e := SelectALAC("https://example.com/master", `#EXTM3U
#EXT-X-STREAM-INF:CODECS="mp4a.40.2"
aac.m3u8`, "hires"); e == nil {
		t.Fatal("accepted AAC")
	}
}

func TestSelectHighQualityAACWithoutDowngrade(t *testing.T) {
	base := "https://fixture.invalid/release/master.m3u8"
	variants := master + `#EXT-X-STREAM-INF:AVERAGE-BANDWIDTH=132000,CODECS="mp4a.40.2",AUDIO="audio-stereo-128"
low.m3u8
#EXT-X-STREAM-INF:AVERAGE-BANDWIDTH=72000,CODECS="mp4a.40.5",AUDIO="audio-stereo-64"
he.m3u8
`
	if _, err := SelectAAC(base, variants); err == nil {
		t.Fatal("accepted a rendition other than stereo AAC 256")
	}
	want := "https://fixture.invalid/release/high.m3u8"
	got, err := SelectAAC(base, variants+`#EXT-X-STREAM-INF:AVERAGE-BANDWIDTH=264000,BANDWIDTH=310000,CODECS="mp4a.40.2",AUDIO="audio-stereo-256"
high.m3u8
`)
	if err != nil || got != want {
		t.Fatalf("got %q, %v; want %q", got, err, want)
	}
}
