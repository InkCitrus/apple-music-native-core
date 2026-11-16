package native

import (
	"io"
	"time"
)

type nativeConnection interface {
	io.ReadWriteCloser
	SetDeadline(time.Time) error
}
