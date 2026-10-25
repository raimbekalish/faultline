package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"

	"github.com/raimbekalish/faultline/internal/history"
)

func executeHistory(ctx context.Context, args []string, out, stderr io.Writer) (code int) {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: faultline history {list|show|compare} [-db path] [run IDs]")
		return 2
	}
	command := args[0]
	if command != "list" && command != "show" && command != "compare" {
		fmt.Fprintln(stderr, "unknown history command")
		return 2
	}
	fs := flag.NewFlagSet("history "+command, flag.ContinueOnError)
	fs.SetOutput(stderr)
	path := fs.String("db", defaultDB, "existing local history file")
	limit := 20
	if command == "list" {
		fs.IntVar(&limit, "limit", 20, "maximum entries (1–100)")
	}
	if err := fs.Parse(args[1:]); err != nil {
		return 2
	}
	want := map[string]int{"list": 0, "show": 1, "compare": 2}[command]
	if fs.NArg() != want || limit < 1 || limit > 100 {
		fmt.Fprintln(stderr, "invalid history arguments")
		return 2
	}
	query, cancel := context.WithTimeout(ctx, history.OperationTimeout)
	defer cancel()
	store, err := history.OpenExisting(query, *path)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer func() {
		if err := store.Close(); err != nil {
			fmt.Fprintln(stderr, err)
			code = 1
		}
	}()
	var value any
	switch command {
	case "list":
		value, err = store.List(query, limit)
	case "show":
		value, err = store.Get(query, fs.Arg(0))
	case "compare":
		value, err = store.Compare(query, fs.Arg(0), fs.Arg(1))
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	if err := enc.Encode(value); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}
