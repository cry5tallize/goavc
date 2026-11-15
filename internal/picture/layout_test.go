package picture

import (
	"errors"
	"testing"

	h264 "github.com/cry5tallize/goavc"
)

func TestNew_RejectsUnsupportedLayout(t *testing.T) {
	for _, format := range []h264.PixelFormat{255} {
		t.Run(format.String(), func(t *testing.T) {
			if _, err := New(1, 1, format); !errors.Is(err, h264.ErrUnsupported) {
				t.Fatalf("New(%s) = %v, want ErrUnsupported", format, err)
			}
			pool := NewPool(1)
			if _, err := pool.Get(1, 1, format); !errors.Is(err, h264.ErrUnsupported) {
				t.Fatalf("Get(%s) = %v, want ErrUnsupported", format, err)
			}
		})
	}
}
