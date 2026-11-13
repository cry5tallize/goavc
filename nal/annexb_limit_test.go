package nal

import (
	"bytes"
	"errors"
	"io"
	"testing"

	h264 "github.com/goavc/h264"
)

func TestAnnexBScanner_NALLimit(t *testing.T) {
	for _, tc := range []struct {
		name  string
		data  []byte
		limit int
		want  error
	}{
		{"exact", []byte{0, 0, 1, 0x65, 0x80}, 2, nil},
		{"overflow", []byte{0, 0, 1, 0x65, 0x40, 0x80}, 2, h264.ErrResourceLimit},
		{"internal_zeros", []byte{0, 0, 1, 0x65, 0, 0, 3, 0x80}, 4, h264.ErrResourceLimit},
		{"tail_zeros", []byte{0, 0, 1, 0x65, 0x80, 0, 0, 0, 0}, 2, nil},
		{"next_prefix", []byte{0, 0, 1, 0x65, 0x80, 0, 0, 0, 1, 0x65, 0x80}, 2, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := NewAnnexBScannerWithLimit(bytes.NewReader(tc.data), tc.limit)
			for {
				_, err := s.Next()
				if errors.Is(err, io.EOF) {
					if tc.want != nil {
						t.Fatal("missing limit error")
					}
					break
				}
				if err != nil {
					if !errors.Is(err, tc.want) {
						t.Fatal(err)
					}
					if _, again := s.Next(); !errors.Is(again, tc.want) {
						t.Fatal("limit error must be sticky")
					}
					break
				}
			}
		})
	}
}
