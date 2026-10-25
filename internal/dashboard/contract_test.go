package dashboard

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"testing"

	"github.com/raimbekalish/faultline/internal/bundle"
)

func TestFrozenV1APIAndHistoryContracts(t *testing.T) {
	s, h, path := setup(t)
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	fixtureDir := "../contracts/testdata/v1/"
	assert := func(name string, got []byte) {
		t.Helper()
		want, err := os.ReadFile(fixtureDir + name + ".json")
		if err != nil {
			t.Fatal(err)
		}
		var a, b bytes.Buffer
		if err = json.Compact(&a, got); err != nil {
			t.Fatal(err)
		}
		if err = json.Compact(&b, want); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(a.Bytes(), b.Bytes()) {
			t.Fatalf("%s contract changed: %s", name, got)
		}
	}
	ids := map[string]string{}
	for _, n := range []string{"complete", "legacy", "canceled"} {
		raw, err := os.ReadFile(fixtureDir + n + ".json")
		if err != nil {
			t.Fatal(err)
		}
		var e bundle.Evidence
		if err = json.Unmarshal(raw, &e); err != nil {
			t.Fatal(err)
		}
		sp, _ := e.Spec.Canonical()
		_, err = db.Exec("INSERT INTO runs(id,engine_run_id,spec_hash,spec_json,created_at,finished_at,status,report_json) VALUES(?,?,?,?,?,?,?,?)", e.History.ID, e.History.RunID, e.History.SpecHash, string(sp), e.History.CreatedAt.Format("2006-01-02T15:04:05.000000000Z"), e.History.FinishedAt.Format("2006-01-02T15:04:05.000000000Z"), e.History.Status, string(e.Report))
		if err != nil {
			t.Fatal(err)
		}
		ids[n] = e.History.ID
		got, err := s.Get(context.Background(), e.History.ID)
		if err != nil {
			t.Fatal(err)
		}
		b, err := json.Marshal(got)
		if err != nil {
			t.Fatal(err)
		}
		assert(n+".history", b)
	}
	for name, url := range map[string]string{"health": "/api/v1/health", "list": "/api/v1/runs?limit=2", "detail": "/api/v1/runs/" + ids["complete"], "partial": "/api/v1/runs/" + ids["canceled"], "comparison": "/api/v1/compare?left=" + ids["complete"] + "&right=" + ids["legacy"], "missing": "/api/v1/runs/history-00000000000000000000000000000000"} {
		w := request(h, url)
		wantCode := 200
		if name == "missing" {
			wantCode = 404
		}
		if w.Code != wantCode {
			t.Fatal(w.Code, w.Body.String())
		}
		assert("api."+name, w.Body.Bytes())
	}
}
