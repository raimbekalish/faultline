package lab

import (
	"strings"
	"testing"
)

func TestPresenceQueriesStayExactReadOnly(t *testing.T) {
	id := strings.Repeat("a", 64)
	for _, op := range []operation{containerPresence, networkPresence} {
		args, e := (readCommand{op, privateSocket, id}).args()
		if e != nil {
			t.Fatal(e)
		}
		joined := strings.Join(args, " ")
		if !strings.Contains(joined, "ls ") || !strings.Contains(joined, "--filter=id="+id) || !strings.Contains(joined, "--no-trunc") {
			t.Fatal(args)
		}
		for _, bad := range []string{"", "foreign-name", "--all", id + " other"} {
			if _, e := (readCommand{op, privateSocket, bad}).args(); e == nil {
				t.Fatal("arbitrary query")
			}
		}
		if _, e := (readCommand{op, "tcp://127.0.0.1:2375", id}).args(); e == nil {
			t.Fatal("remote")
		}
	}
}
