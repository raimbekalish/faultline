package main

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestFixtureValueAndCount(t *testing.T) {
	h := fixture()
	for i := 0; i < 3; i++ {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "/value", nil))
		if w.Code != 200 || w.Body.String() != "{\"value\":42}\n" {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/count", nil))
	var count struct {
		Requests int `json:"requests"`
	}
	if json.Unmarshal(w.Body.Bytes(), &count) != nil || count.Requests != 3 {
		t.Fatal(w.Body.String())
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/value", nil))
	if w.Code != 405 {
		t.Fatal("unexpected method")
	}
}
