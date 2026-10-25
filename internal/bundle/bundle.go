// Package bundle verifies portable evidence without SQLite, Docker or collector access.
package bundle

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"regexp"

	"time"

	"github.com/raimbekalish/faultline/internal/experiment"
	"github.com/raimbekalish/faultline/internal/jsonstrict"
	"github.com/raimbekalish/faultline/internal/record"
	"github.com/raimbekalish/faultline/internal/spec"
)

const MaxArchive = 2 << 20
const MaxReport = 1 << 20

var names = []string{"manifest.json", "history.json", "spec.json", "report.json"}
var limits = map[string]int{"manifest.json": 4096, "history.json": 4096, "spec.json": 4096, "report.json": MaxReport}
var historyID = regexp.MustCompile(`^history-[a-f0-9]{32}$`)

type Metadata struct {
	ID         string    `json:"id"`
	RunID      string    `json:"run_id,omitempty"`
	SpecHash   string    `json:"spec_hash"`
	CreatedAt  time.Time `json:"created_at"`
	FinishedAt time.Time `json:"finished_at"`
	Status     string    `json:"status"`
}
type Evidence struct {
	History Metadata        `json:"history"`
	Spec    spec.Spec       `json:"spec"`
	Report  json.RawMessage `json:"report"`
}
type Digest struct {
	Name   string `json:"name"`
	Size   int    `json:"size"`
	SHA256 string `json:"sha256"`
}
type Manifest struct {
	Format  string   `json:"format"`
	Version int      `json:"format_version"`
	Files   []Digest `json:"files"`
}
type Inspection struct {
	Version   int    `json:"format_version"`
	Integrity string `json:"integrity"`
	Evidence
	Warnings []string `json:"warnings"`
}

func digest(name string, b []byte) Digest {
	h := sha256.Sum256(b)
	return Digest{name, len(b), hex.EncodeToString(h[:])}
}
func (e Evidence) Validate() error {
	if !historyID.MatchString(e.History.ID) || e.History.CreatedAt.IsZero() || e.History.FinishedAt.IsZero() || e.History.FinishedAt.Before(e.History.CreatedAt) || len(e.Report) > MaxReport {
		return fmt.Errorf("invalid history metadata or report size")
	}
	if err := e.Spec.Validate(); err != nil {
		return err
	}
	hash, _ := e.Spec.Hash()
	if hash != e.History.SpecHash {
		return fmt.Errorf("specification hash mismatch")
	}
	if err := validJSON(e.Report); err != nil {
		return err
	}
	var r experiment.Report
	if err := json.Unmarshal(e.Report, &r); err != nil {
		return err
	}
	if err := record.Validate(r, e.Spec); err != nil {
		return err
	}
	if r.Status != e.History.Status || r.RunID != e.History.RunID {
		return fmt.Errorf("report/history identity mismatch")
	}
	return nil
}
func Build(ctx context.Context, e Evidence) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := e.Validate(); err != nil {
		return nil, err
	}
	metadata, _ := json.Marshal(e.History)
	canonical, _ := e.Spec.Canonical()
	contents := map[string][]byte{"history.json": metadata, "spec.json": canonical, "report.json": e.Report}
	m := Manifest{Format: "faultline-evidence", Version: 1, Files: make([]Digest, 0, 3)}
	for _, name := range names[1:] {
		m.Files = append(m.Files, digest(name, contents[name]))
	}
	contents[names[0]], _ = json.Marshal(m)
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		header := &zip.FileHeader{Name: name, Method: zip.Store, ModifiedDate: 33}
		header.SetMode(0600)
		w, err := z.CreateHeader(header)
		if err != nil {
			return nil, err
		}
		if err = writeBytes(ctx, w, contents[name]); err != nil {
			return nil, err
		}
	}
	if err := z.Close(); err != nil {
		return nil, err
	}
	if b.Len() > MaxArchive {
		return nil, fmt.Errorf("archive too large")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}
func Read(ctx context.Context, reader io.Reader) (Inspection, error) {
	var result Inspection
	b, err := io.ReadAll(io.LimitReader(&contextReader{ctx, reader}, MaxArchive+1))
	if err != nil {
		return result, err
	}
	if len(b) > MaxArchive || len(b) < 22 {
		return result, fmt.Errorf("invalid archive size")
	}
	// Version 1 uses a plain, single-disk, non-ZIP64, comment-free stored ZIP.
	end := b[len(b)-22:]
	if !bytes.Equal(end[:4], []byte{'P', 'K', 5, 6}) || binary.LittleEndian.Uint16(end[4:6]) != 0 || binary.LittleEndian.Uint16(end[6:8]) != 0 || binary.LittleEndian.Uint16(end[8:10]) != 4 || binary.LittleEndian.Uint16(end[10:12]) != 4 || binary.LittleEndian.Uint16(end[20:22]) != 0 {
		return result, fmt.Errorf("unsupported ZIP layout")
	}
	directory := uint64(binary.LittleEndian.Uint32(end[16:20]))
	directorySize := uint64(binary.LittleEndian.Uint32(end[12:16]))
	if directory+directorySize != uint64(len(b)-22) {
		return result, fmt.Errorf("invalid ZIP directory")
	}
	expectedDirectorySize := 0
	for _, name := range names {
		expectedDirectorySize += 46 + len(name)
	}
	if directorySize != uint64(expectedDirectorySize) {
		return result, fmt.Errorf("unsupported directory size")
	}
	z, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		return result, err
	}
	if len(z.File) != 4 || z.Comment != "" {
		return result, fmt.Errorf("expected exactly four evidence entries")
	}
	files := map[string][]byte{}
	cursor := uint64(0)
	for index, f := range z.File {
		limit, ok := limits[f.Name]
		if !ok || f.Name != names[index] || files[f.Name] != nil || !f.Mode().IsRegular() || f.Method != zip.Store || f.Flags & ^uint16(8) != 0 || len(f.Extra) != 0 || f.Comment != "" || f.UncompressedSize64 > uint64(limit) || f.CompressedSize64 != f.UncompressedSize64 {
			return result, fmt.Errorf("unsafe or unsupported archive entry")
		}
		offset, err := f.DataOffset()
		if err != nil {
			return result, err
		}
		// Do not extract. Reject hidden local entries, mismatched names and prefixes.
		if cursor+30+uint64(len(f.Name)) > uint64(len(b)) || offset != int64(cursor+30+uint64(len(f.Name))) {
			return result, fmt.Errorf("invalid local entry offset")
		}
		header := b[cursor : cursor+30]
		if !bytes.Equal(header[:4], []byte{'P', 'K', 3, 4}) || binary.LittleEndian.Uint16(header[6:8]) != f.Flags || binary.LittleEndian.Uint16(header[8:10]) != zip.Store || binary.LittleEndian.Uint16(header[26:28]) != uint16(len(f.Name)) || binary.LittleEndian.Uint16(header[28:30]) != 0 || string(b[cursor+30:uint64(offset)]) != f.Name {
			return result, fmt.Errorf("local/central header mismatch")
		}
		cursor = uint64(offset) + f.CompressedSize64
		if f.Flags&8 != 0 {
			if cursor+16 > uint64(len(b)) {
				return result, fmt.Errorf("truncated descriptor")
			}
			d := b[cursor : cursor+16]
			if !bytes.Equal(d[:4], []byte{'P', 'K', 7, 8}) || binary.LittleEndian.Uint32(d[4:8]) != f.CRC32 || uint64(binary.LittleEndian.Uint32(d[8:12])) != f.CompressedSize64 || uint64(binary.LittleEndian.Uint32(d[12:16])) != f.UncompressedSize64 {
				return result, fmt.Errorf("invalid entry descriptor")
			}
			cursor += 16
		} else if binary.LittleEndian.Uint32(header[14:18]) != f.CRC32 || uint64(binary.LittleEndian.Uint32(header[18:22])) != f.CompressedSize64 || uint64(binary.LittleEndian.Uint32(header[22:26])) != f.UncompressedSize64 {
			return result, fmt.Errorf("invalid local entry sizes")
		}
		if cursor > directory {
			return result, fmt.Errorf("entry overlaps directory")
		}
		r, err := f.Open()
		if err != nil {
			return result, err
		}
		data, readErr := io.ReadAll(io.LimitReader(&contextReader{ctx, r}, int64(limit+1)))
		closeErr := r.Close()
		if readErr != nil {
			return result, readErr
		}
		if closeErr != nil {
			return result, closeErr
		}
		if len(data) > limit || uint64(len(data)) != f.UncompressedSize64 {
			return result, fmt.Errorf("entry size mismatch")
		}
		files[f.Name] = data
	}
	if cursor != directory {
		return result, fmt.Errorf("hidden archive data")
	}
	var m Manifest
	if err := canonicalJSON(files[names[0]], &m); err != nil {
		return result, err
	}
	if m.Format != "faultline-evidence" || m.Version != 1 {
		return result, fmt.Errorf("unsupported evidence format version")
	}
	if len(m.Files) != 3 {
		return result, fmt.Errorf("invalid digest manifest")
	}
	for i, name := range names[1:] {
		if m.Files[i] != digest(name, files[name]) {
			return result, fmt.Errorf("checksum or size mismatch: %s", name)
		}
	}
	if err := canonicalJSON(files["history.json"], &result.History); err != nil {
		return result, err
	}
	result.Spec, err = spec.Decode(bytes.NewReader(files["spec.json"]))
	if err != nil {
		return result, err
	}
	canonical, _ := result.Spec.Canonical()
	if !bytes.Equal(canonical, files["spec.json"]) {
		return result, fmt.Errorf("noncanonical specification")
	}
	result.Report = append(json.RawMessage(nil), files["report.json"]...)
	if err := result.Evidence.Validate(); err != nil {
		return Inspection{}, err
	}
	if err := ctx.Err(); err != nil {
		return Inspection{}, err
	}
	result.Version = 1
	result.Integrity = "checksums_verified_not_authenticity"
	result.Warnings = warnings(result.Evidence)
	return result, nil
}
func canonicalJSON(b []byte, dst any) error {
	if err := validJSON(b); err != nil {
		return err
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(dst); err != nil {
		return err
	}
	canonical, err := json.Marshal(dst)
	if err != nil || !bytes.Equal(b, canonical) {
		return fmt.Errorf("noncanonical metadata")
	}
	return nil
}

func validJSON(b []byte) error { return jsonstrict.Validate(b) }
func warnings(e Evidence) []string {
	var r experiment.Report
	_ = json.Unmarshal(e.Report, &r)
	out := []string{"Checksums detect inconsistency; they do not establish authenticity.", "Trace/span IDs are references only; temporary Jaeger traces are not included."}
	if r.Environment == nil {
		out = append(out, "Environment provenance was not recorded; it cannot be reconstructed by export.")
	} else if r.Environment.Launch == nil {
		out = append(out, "Docker server/image provenance was unavailable at execution.")
	}
	if r.Status != "complete" {
		out = append(out, "This report is failed or canceled and may contain partial observations.")
	}
	if !r.CleanupComplete {
		out = append(out, "Fault cleanup was not confirmed in this report.")
	}
	for _, phase := range []struct {
		name string
		p    experiment.Phase
	}{{"baseline", r.Baseline}, {"fault", r.Fault}, {"recovery", r.Recovery}} {
		if phase.p.Summary.Requests < e.Spec.Samples {
			out = append(out, fmt.Sprintf("%s: %d/%d observations recorded; missing samples remain unavailable.", phase.name, phase.p.Summary.Requests, e.Spec.Samples))
		}
	}
	return out
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r *contextReader) Read(b []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(b)
}
func writeBytes(ctx context.Context, w io.Writer, b []byte) error {
	for len(b) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		n := len(b)
		if n > 32768 {
			n = 32768
		}
		written, err := w.Write(b[:n])
		if err != nil {
			return err
		}
		if written != n {
			return io.ErrShortWrite
		}
		b = b[n:]
	}
	return ctx.Err()
}
