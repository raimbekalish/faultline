package jsonstrict

import "testing"

func TestRejectAmbiguousUnicodeAndMalformedStrings(t *testing.T) {
	for _, s := range []string{
		`{"status":1,"ſtatus":2}`, // encoding/json uses Unicode simple folding.
		`{"s":1,"\u017f":2}`,
		`{"error":"\ud800"}`,
		`{"error":"\udc00"}`,
		`{"error":"\ud800\u0041"}`,
		string([]byte{'"', 0xff, '"'}),
	} {
		if err := Validate([]byte(s)); err == nil {
			t.Errorf("accepted ambiguous/invalid JSON: %s", s)
		}
	}
	for _, s := range []string{`{"error":"\ud83d\ude00"}`, `{"error":"😀"}`, `{"error":"\\ud800"}`, `{"status":1,"other":2}`} {
		if err := Validate([]byte(s)); err != nil {
			t.Errorf("rejected valid JSON %s: %v", s, err)
		}
	}
}

func FuzzValidate(f *testing.F) {
	f.Add([]byte(`{"status":1,"ſtatus":2}`))
	f.Add([]byte(`{"error":"\ud83d\ude00"}`))
	f.Fuzz(func(t *testing.T, b []byte) {
		if len(b) <= 1<<20 {
			_ = Validate(b)
		}
	})
}
