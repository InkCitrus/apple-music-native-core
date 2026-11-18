package stream

import (
	"bytes"
	"context"
	"errors"

	"github.com/InkCitrus/apple-music-native-core/codec"
)

func radioID3Size(data []byte) (int, error) {
	if len(data) < 10 {
		return 0, errors.New("truncated broadcast metadata")
	}
	n := 0
	for _, b := range data[6:10] {
		if b >= 128 {
			return 0, errors.New("invalid broadcast metadata size")
		}
		n = (n << 7) | int(b)
	}
	n += 10
	if data[3] == 4 && data[5]&0x10 != 0 {
		n += 10 // Optional ID3v2.4 footer.
	}
	if n > len(data) {
		return 0, errors.New("truncated broadcast metadata")
	}
	return n, nil
}

func radioADTS(data []byte) (config [2]byte, packet []byte, size int, err error) {
	if len(data) < 7 || data[0] != 0xff || data[1]&0xf6 != 0xf0 || data[6]&3 != 0 {
		return config, nil, 0, errors.New("invalid broadcast ADTS frame")
	}
	header := 7
	if data[1]&1 == 0 {
		header = 9
	}
	size = int(data[3]&3)<<11 | int(data[4])<<3 | int(data[5]>>5)
	if size <= header || size > len(data) {
		return config, nil, 0, errors.New("truncated broadcast ADTS payload")
	}
	profile := (data[2] >> 6) + 1
	rate := (data[2] >> 2) & 15
	channels := (data[2]&1)<<2 | data[3]>>6
	config = [2]byte{profile<<3 | rate>>1, rate<<7 | channels<<3}
	return config, data[header:size], size, nil
}

type radioDecoder struct {
	decoder codec.PCMDecoder
	config  [2]byte
	format  codec.Format
}

func (d *radioDecoder) close() {
	if d.decoder != nil {
		d.decoder.Close()
		d.decoder = nil
	}
}

func (d *radioDecoder) decode(ctx context.Context, data []byte, reset bool, onFormat func(codec.Format) error, onPCM func([]byte) error) error {
	if reset {
		d.close()
	}
	frames := 0
	for len(data) != 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		// Apple inserts timed ID3 tags between consecutive ADTS frames.
		if bytes.HasPrefix(data, []byte("ID3")) {
			size, err := radioID3Size(data)
			if err != nil {
				return err
			}
			data = data[size:]
			continue
		}
		config, packet, size, err := radioADTS(data)
		if err != nil {
			return err
		}
		var pcm []byte
		if d.decoder == nil {
			d.decoder, pcm, err = codec.NewLiveAAC(config[:], packet)
			if err != nil {
				return err
			}
			d.config = config
			format := d.decoder.Format()
			switch {
			case d.format.SampleRate != 0 && d.format != format:
				return errors.New("broadcast format changed")
			case d.format.SampleRate == 0 && onFormat != nil:
				if err := onFormat(format); err != nil {
					return err
				}
			}
			d.format = format
		} else if config != d.config {
			return errors.New("broadcast AAC configuration changed")
		} else {
			pcm, err = d.decoder.Decode(packet)
			if err != nil {
				return err
			}
		}
		if onPCM != nil && len(pcm) != 0 {
			if err := onPCM(pcm); err != nil {
				return err
			}
		}
		frames++
		data = data[size:]
	}
	if frames == 0 {
		return errors.New("empty broadcast segment")
	}
	return nil
}
