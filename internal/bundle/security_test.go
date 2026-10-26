package bundle

import (
	"bytes"
	"context"
	"encoding/binary"
	"testing"
)

func TestHeaderConsistency(t *testing.T) {
	good, err := Build(context.Background(), fixture("complete"))
	if err != nil {
		t.Fatal(err)
	}
	dir := int(binary.LittleEndian.Uint32(good[len(good)-6 : len(good)-2]))
	for _, tc := range []struct {
		name   string
		mutate func([]byte)
	}{
		{"central disk", func(b []byte) { binary.LittleEndian.PutUint16(b[dir+34:], 1) }},
		{"local CRC with descriptor", func(b []byte) { binary.LittleEndian.PutUint32(b[14:], 123) }},
		{"local size with descriptor", func(b []byte) { binary.LittleEndian.PutUint32(b[18:], 123) }},
		{"local reader version", func(b []byte) { binary.LittleEndian.PutUint16(b[4:], 45) }},
		{"central reader version", func(b []byte) { binary.LittleEndian.PutUint16(b[dir+6:], 45) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := bytes.Clone(good)
			tc.mutate(b)
			if _, err := Read(context.Background(), bytes.NewReader(b)); err == nil {
				t.Fatal("inconsistent ZIP header accepted")
			}
		})
	}
}

func TestReadCancellationAndShortRead(t *testing.T) {
	good, _ := Build(context.Background(), fixture("complete"))
	ctx, cancel := context.WithCancel(context.Background())
	if _, err := Read(ctx, &cancelReader{bytes.NewReader(good), cancel}); err == nil {
		t.Fatal("cancellation ignored")
	}
	for _, n := range []int{1, 29, len(good) / 2, len(good) - 22} {
		if _, err := Read(context.Background(), bytes.NewReader(good[:n])); err == nil {
			t.Fatal("short archive accepted", n)
		}
	}
}

type cancelReader struct {
	*bytes.Reader
	cancel context.CancelFunc
}

func (r *cancelReader) Read(b []byte) (int, error) {
	n, err := r.Reader.Read(b)
	r.cancel()
	return n, err
}
