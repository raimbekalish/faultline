package buildinfo

import (
	"bytes"
	"encoding/json"
	"io"
	"testing"
)

type broken struct{}

func (broken) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }
func TestMetadata(t *testing.T) {
	var a, b bytes.Buffer
	if err := Write(&a); err != nil {
		t.Fatal(err)
	}
	if err := Write(&b); err != nil {
		t.Fatal(err)
	}
	if a.String() != b.String() {
		t.Fatal("unstable metadata")
	}
	var m map[string]string
	if err := json.Unmarshal(a.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	if len(m) != 6 || m["version"] == "" || m["revision"] == "" || m["go_version"] == "" || m["os"] == "" || m["arch"] == "" {
		t.Fatal(m)
	}
	if err := Write(broken{}); err == nil {
		t.Fatal("output error ignored")
	}
}
