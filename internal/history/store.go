package history

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/raimbekalish/faultline/internal/experiment"
	"github.com/raimbekalish/faultline/internal/spec"
)

var ErrNotFound = errors.New("history run not found")
var ErrFinalized = errors.New("history run already finalized")
var ErrCorrupt = errors.New("malformed history record")
var entryID = regexp.MustCompile(`^history-[a-f0-9]{32}$`)
var engineID = regexp.MustCompile(`^faultline-[a-f0-9]{32}$`)

const timestampLayout = "2006-01-02T15:04:05.000000000Z"
const maxReport = 1 << 20

type Entry struct {
	ID         string          `json:"id"`
	RunID      string          `json:"run_id,omitempty"`
	SpecHash   string          `json:"spec_hash"`
	Spec       spec.Spec       `json:"spec"`
	CreatedAt  time.Time       `json:"created_at"`
	FinishedAt *time.Time      `json:"finished_at,omitempty"`
	Status     string          `json:"status"`
	Report     json.RawMessage `json:"report,omitempty"`
}

// Start commits the intent before any engine call. No transaction spans a fault.
func (s *Store) Start(ctx context.Context, sp spec.Spec) (string, error) {
	canonical, err := sp.Canonical()
	if err != nil {
		return "", err
	}
	hash, _ := sp.Hash()
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	id := "history-" + hex.EncodeToString(random[:])
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "INSERT INTO runs(id,spec_hash,spec_json,created_at,status) VALUES(?,?,?,?, 'running')", id, hash, string(canonical), time.Now().UTC().Format(timestampLayout)); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return id, nil
}

// Finish atomically replaces only a running intent, after engine cleanup. A
// failed transaction leaves that intent intact; final reports cannot be overwritten.
func (s *Store) Finish(ctx context.Context, id string, r experiment.Report) error {
	if !entryID.MatchString(id) {
		return fmt.Errorf("invalid history ID")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	e, err := scanEntry(tx.QueryRowContext(ctx, selectEntry+" WHERE id=?", id))
	if err != nil {
		return err
	}
	if e.Status != "running" {
		return ErrFinalized
	}
	if err := validateReport(r, e.Spec); err != nil {
		return err
	}
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	if len(b) > maxReport {
		return fmt.Errorf("report exceeds %d bytes", maxReport)
	}
	var runID any
	if r.RunID != "" {
		runID = r.RunID
	}
	result, err := tx.ExecContext(ctx, "UPDATE runs SET engine_run_id=?,finished_at=?,status=?,report_json=? WHERE id=? AND status='running'", runID, time.Now().UTC().Format(timestampLayout), r.Status, string(b), id)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrFinalized
	}
	return tx.Commit()
}

const selectEntry = `SELECT id,engine_run_id,spec_hash,
 CASE WHEN length(CAST(spec_json AS BLOB))<=4096 THEN spec_json ELSE 'oversized' END,
 created_at,finished_at,status,
 CASE WHEN length(CAST(report_json AS BLOB))<=1048576 THEN report_json WHEN report_json IS NULL THEN NULL ELSE 'oversized' END FROM runs`

type scanner interface{ Scan(...any) error }

func scanEntry(row scanner) (Entry, error) {
	var e Entry
	var runID, finished, report sql.NullString
	var snapshot, created string
	err := row.Scan(&e.ID, &runID, &e.SpecHash, &snapshot, &created, &finished, &e.Status, &report)
	if errors.Is(err, sql.ErrNoRows) {
		return e, ErrNotFound
	}
	if err != nil {
		return e, err
	}
	bad := func() (Entry, error) { return e, fmt.Errorf("%w: %s", ErrCorrupt, e.ID) }
	if !entryID.MatchString(e.ID) {
		return bad()
	}
	e.RunID = runID.String
	e.Spec, err = spec.Decode(bytes.NewBufferString(snapshot))
	if err != nil {
		return bad()
	}
	hash, _ := e.Spec.Hash()
	if hash != e.SpecHash {
		return bad()
	}
	e.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
	if err != nil || e.CreatedAt.IsZero() {
		return bad()
	}
	if e.Status == "running" {
		if finished.Valid || report.Valid || runID.Valid {
			return bad()
		}
		return e, nil
	}
	if !finished.Valid || !report.Valid {
		return bad()
	}
	t, err := time.Parse(time.RFC3339Nano, finished.String)
	if err != nil || t.Before(e.CreatedAt) {
		return bad()
	}
	e.FinishedAt = &t
	var r experiment.Report
	if err := json.Unmarshal([]byte(report.String), &r); err != nil {
		return bad()
	}
	if validateReport(r, e.Spec) != nil || r.Status != e.Status || r.RunID != e.RunID {
		return bad()
	}
	e.Report = json.RawMessage(report.String)
	return e, nil
}

func (s *Store) Get(ctx context.Context, id string) (Entry, error) {
	if !entryID.MatchString(id) && !engineID.MatchString(id) {
		return Entry{}, fmt.Errorf("invalid run ID")
	}
	return scanEntry(s.db.QueryRowContext(ctx, selectEntry+" WHERE id=? OR engine_run_id=?", id, id))
}

func (s *Store) List(ctx context.Context, limit int) ([]Entry, error) {
	if limit < 1 || limit > 100 {
		return nil, fmt.Errorf("limit must be between 1 and 100")
	}
	// Validate each bounded entry, then omit report payloads from list output.
	rows, err := s.db.QueryContext(ctx, "SELECT id FROM runs ORDER BY created_at DESC,id DESC LIMIT ?", limit)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	entries := make([]Entry, 0, len(ids))
	for _, id := range ids {
		e, err := s.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		e.Report = nil
		entries = append(entries, e)
	}
	return entries, nil
}
