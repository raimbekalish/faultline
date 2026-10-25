package history

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Counts describe recorded intent statuses, not inferred process outcomes.
type Counts struct {
	Complete int `json:"complete"`
	Failed   int `json:"failed"`
	Canceled int `json:"canceled"`
	Running  int `json:"running"`
}
type Filter struct {
	Limit                  int
	Cursor, Status, Search string
}
type Page struct {
	Items      []Entry
	Counts     Counts
	Total      int
	NextCursor *string
}

func ValidID(id string) bool { return entryID.MatchString(id) || engineID.MatchString(id) }
func (f Filter) Validate() error {
	if f.Limit < 1 || f.Limit > 50 || (f.Cursor != "" && !entryID.MatchString(f.Cursor)) || len(f.Search) > 128 || !utf8.ValidString(f.Search) || strings.ContainsAny(f.Search, "\x00\r\n") {
		return fmt.Errorf("invalid history filter")
	}
	switch f.Status {
	case "", "complete", "failed", "canceled", "running":
	default:
		return fmt.Errorf("invalid history status")
	}
	return nil
}

// Browse uses one SQLite read snapshot. Report validation is shared with Get;
// no transaction or connection escapes the query. Only metadata is aggregated.
func (s *Store) Browse(ctx context.Context, f Filter) (Page, error) {
	var p Page
	if err := f.Validate(); err != nil {
		return p, err
	}
	// database/sql read-only options are not enforced by every SQLite driver.
	// This transaction issues SELECTs only; it never finalizes or migrates runs.
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return p, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, "SELECT status,count(*) FROM runs GROUP BY status")
	if err != nil {
		return p, err
	}
	for rows.Next() {
		var status string
		var n int
		if err = rows.Scan(&status, &n); err != nil {
			break
		}
		switch status {
		case "complete":
			p.Counts.Complete = n
		case "failed":
			p.Counts.Failed = n
		case "canceled":
			p.Counts.Canceled = n
		case "running":
			p.Counts.Running = n
		default:
			err = ErrCorrupt
		}
		if err != nil {
			break
		}
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return p, err
	}
	where := " WHERE (?='' OR status=?) AND (?='' OR instr(lower(id || ' ' || coalesce(engine_run_id,'') || ' ' || spec_json),lower(?))>0)"
	args := []any{f.Status, f.Status, f.Search, f.Search}
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM runs"+where, args...).Scan(&p.Total); err != nil {
		return p, err
	}
	if f.Cursor != "" {
		anchor, e := scanEntry(tx.QueryRowContext(ctx, selectEntry+" WHERE id=?", f.Cursor))
		if e != nil {
			return p, e
		}
		// Preserve the stored timestamp representation for SQL's lexical ordering.
		var created string
		if e = tx.QueryRowContext(ctx, "SELECT created_at FROM runs WHERE id=?", anchor.ID).Scan(&created); e != nil {
			return p, e
		}
		where += " AND (created_at<? OR (created_at=? AND id<?))"
		args = append(args, created, created, anchor.ID)
	}
	args = append(args, f.Limit+1)
	rows, err = tx.QueryContext(ctx, selectEntry+where+" ORDER BY created_at DESC,id DESC LIMIT ?", args...)
	if err != nil {
		return p, err
	}
	p.Items = make([]Entry, 0, f.Limit)
	for rows.Next() {
		e, eerr := scanEntry(rows)
		if eerr != nil {
			err = eerr
			break
		}
		p.Items = append(p.Items, e)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return Page{}, err
	}
	if len(p.Items) > f.Limit {
		p.Items = p.Items[:f.Limit]
		id := p.Items[len(p.Items)-1].ID
		p.NextCursor = &id
	}
	if err = tx.Commit(); err != nil {
		return Page{}, err
	}
	return p, nil
}
