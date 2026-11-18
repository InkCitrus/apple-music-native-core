//go:build !amcore_aac

package codec

import "errors"

const HasAAC = false

func NewAAC(config []byte) (PCMDecoder, error) {
	return nil, errors.New("AAC decoder was not included in this build")
}

func NewLiveAAC(config, firstPacket []byte) (PCMDecoder, []byte, error) {
	return nil, nil, errors.New("AAC decoder was not included in this build")
}
