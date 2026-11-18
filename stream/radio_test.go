package stream

import (
	"net/url"
	"strings"
	"testing"
)

func TestRadioPlaylistRejectsUnsupportedSegments(t *testing.T) {
	base, _ := url.Parse("https://itsliveradio.apple.com/live/media.m3u8")
	prefix := "#EXTM3U\n#EXT-X-MEDIA-SEQUENCE:99\n#EXT-X-TARGETDURATION:16\n"
	segments := "#EXTINF:16,\n99.aac\n#EXT-X-DISCONTINUITY\n#EXTINF:16,\n100.aac\n#EXTINF:16,\n101.aac\n"
	p, err := parseRadioPlaylist([]byte(prefix+segments), base)
	if err != nil {
		t.Fatal(err)
	}
	if p.start() != 100 || !p.segs[1].discontinuity || p.segs[1].url.Path != "/live/100.aac" {
		t.Fatal("wrong live edge or segment reference")
	}
	for _, invalid := range []string{
		prefix + "#EXT-X-KEY:METHOD=SAMPLE-AES,URI=\"skd://fixture.invalid/key\"\n" + segments,
		prefix + segments + "#EXT-X-ENDLIST\n",
		prefix + strings.Replace(segments, "99.aac", "https://fixture.invalid/99.aac", 1),
		prefix + strings.Replace(segments, "99.aac", "http://itsliveradio.apple.com/99.aac", 1),
		prefix + strings.Replace(segments, "99.aac", "https://user@itsliveradio.apple.com/99.aac", 1),
	} {
		if _, err := parseRadioPlaylist([]byte(invalid), base); err == nil {
			t.Fatal("accepted an unsupported radio playlist")
		}
	}
}
