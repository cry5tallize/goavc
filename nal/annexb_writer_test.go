package nal

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

func TestAnnexBWriter_ExactBytesAndRescan(t *testing.T) {
	var b bytes.Buffer
	u := Unit{Header: Header{Type: TypeSliceIDR, RefIDC: 3}, Payload: []byte{0x80}}
	if err := NewAnnexBWriter(&b).Write(u); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(b.Bytes(), []byte{0, 0, 0, 1, 0x65, 0x80}) {
		t.Fatal(b.Bytes())
	}
	s := NewAnnexBScanner(&b)
	got, err := s.Next()
	if err != nil || got.Header != u.Header || !bytes.Equal(got.Payload, u.Payload) {
		t.Fatalf("%+v %v", got, err)
	}
	if _, err = s.Next(); !errors.Is(err, io.EOF) {
		t.Fatal(err)
	}
	if err = NewAnnexBWriter(shortWriter{}).Write(u); !errors.Is(err, io.ErrShortWrite) {
		t.Fatal(err)
	}
}
