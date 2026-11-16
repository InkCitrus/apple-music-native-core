package codec

import (
	"bytes"
	"encoding/binary"
	"github.com/Eyevinn/mp4ff/mp4"
	"os"
	"testing"
)

func TestSyntheticPCMBitExact(t *testing.T) {
	for _, name := range []string{"pcm-16-44100", "pcm-24-96000", "pcm-24-192000"} {
		t.Run(name, func(t *testing.T) {
			data, e := os.ReadFile("testdata/" + name + ".m4a")
			if e != nil {
				t.Fatal(e)
			}
			file, e := mp4.DecodeFile(bytes.NewReader(data))
			if e != nil {
				t.Fatal(e)
			}
			track := file.Moov.Traks[0]
			entry := track.Mdia.Minf.Stbl.Stsd.Children[0].(*mp4.UnknownBox).Payload()
			var cookie []byte
			for p := 28; p+8 <= len(entry); {
				n := int(binary.BigEndian.Uint32(entry[p : p+4]))
				if n < 8 || p+n > len(entry) {
					t.Fatal("invalid fixture")
				}
				if string(entry[p+4:p+8]) == "alac" {
					cookie = entry[p+12 : p+36]
					break
				}
				p += n
			}
			d, e := New(cookie)
			if e != nil {
				t.Fatal(e)
			}
			defer d.Close()
			var pcm []byte
			for i := uint32(1); i <= track.GetNrSamples(); i++ {
				r, e := track.GetRangesForSampleInterval(i, i)
				if e != nil || len(r) != 1 {
					t.Fatal("invalid fixture sample")
				}
				chunk, e := d.Decode(data[r[0].Offset : r[0].Offset+r[0].Size])
				if e != nil {
					t.Fatal(e)
				}
				pcm = append(pcm, chunk...)
			}
			want, e := os.ReadFile("testdata/" + name + ".pcm")
			if e != nil {
				t.Fatal(e)
			}
			if !bytes.Equal(pcm, want) {
				t.Fatal("decoded PCM differs from synthesized source")
			}
		})
	}
}
func TestRejectInvalidCookie(t *testing.T) {
	if _, err := New(make([]byte, 24)); err == nil {
		t.Fatal("accepted invalid ALAC configuration")
	}
}
