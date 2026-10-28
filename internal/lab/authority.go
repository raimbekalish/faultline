package lab

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/raimbekalish/faultline/internal/jsonstrict"
)

const authorityDirectory = ".faultline-supervisor-v1"
const enrollmentName = ".faultline-supervisor-v1.anchor.json"
const MaxAuthorityRuns = 32

var ErrAuthority = errors.New("authoritative supervisor state missing, corrupt or unresolved")
var ErrLease = errors.New("custom lab reservation unresolved or process lease busy")

type enrollment struct {
	SchemaVersion int    `json:"schema_version"`
	Kind          string `json:"kind"`
	Nonce         string `json:"nonce"`
}
type registryRecord struct {
	Sequence      int    `json:"sequence"`
	Previous      string `json:"previous_sha256"`
	Authority     string `json:"authority_nonce"`
	Action        string `json:"action"`
	LaunchID      string `json:"launch_id"`
	PlanSHA256    string `json:"plan_sha256"`
	RuntimeKey    string `json:"runtime_key"`
	JournalSHA256 string `json:"journal_sha256"`
}
type registryHead struct {
	Sequence int    `json:"sequence"`
	SHA256   string `json:"sha256"`
}
type reservation struct {
	record   registryRecord
	closed   bool
	imported bool
}
type Authority struct {
	home         *os.Root
	store        *JournalStore
	seal         enrollment
	closed       atomic.Bool
	probeRuntime func(context.Context, Plan) (runtimeWitness, error)
}

// Account lookup is by effective UID, never HOME/XDG or a caller's directory.
// Native CLI already requires CGO. Unsupported account lookup fails closed.
func authoritativeHome() (string, error) {
	u, e := user.LookupId(strconv.Itoa(os.Geteuid()))
	if e != nil || u.Uid != strconv.Itoa(os.Geteuid()) || !filepath.IsAbs(u.HomeDir) {
		return "", ErrAuthority
	}
	return u.HomeDir, nil
}
func authorityHome(home string) (*os.Root, error) {
	a, e := filepath.Abs(home)
	if e != nil {
		return nil, ErrAuthority
	}
	c, e := filepath.EvalSymlinks(a)
	if e != nil || a != c {
		return nil, ErrAuthority
	}
	r, e := os.OpenRoot(a)
	if e != nil {
		return nil, ErrAuthority
	}
	st, e := r.Stat(".")
	if e != nil || !st.IsDir() || !ownedDirectory(st) || st.Mode().Perm()&0022 != 0 {
		r.Close()
		return nil, ErrAuthority
	}
	return r, nil
}

// InitializeAuthority is explicit enrollment only, with no state-path option.
// It is not wired into any CLI or performed by Open/Begin. An orphan anchor or
// directory blocks re-enrollment; no automatic reset/import/discovery exists.
func InitializeAuthority(ctx context.Context) error {
	h, e := authoritativeHome()
	if e != nil {
		return e
	}
	return initializeAuthorityAt(ctx, h)
}
func initializeAuthorityAt(ctx context.Context, home string) error {
	r, e := authorityHome(home)
	if e != nil {
		return e
	}
	defer r.Close()
	if _, e = r.Lstat(authorityDirectory); !errors.Is(e, os.ErrNotExist) {
		return ErrAuthority
	}
	var token [32]byte
	if _, e = rand.Read(token[:]); e != nil {
		return ErrAuthority
	}
	seal := enrollment{1, "faultline-supervisor-enrollment", hex.EncodeToString(token[:])}
	b, _ := json.Marshal(seal)
	if publish(ctx, r, enrollmentName, b) != nil {
		return ErrAuthority
	}
	if r.Mkdir(authorityDirectory, 0700) != nil {
		return ErrAuthority
	}
	state, e := r.OpenRoot(authorityDirectory)
	if e != nil {
		return ErrAuthority
	}
	defer state.Close()
	if publish(ctx, state, "authority.json", b) != nil {
		return ErrAuthority
	}
	first := registryRecord{Sequence: 0, Authority: seal.Nonce, Action: "genesis"}
	raw, _ := json.Marshal(first)
	if publish(ctx, state, registryName(0), raw) != nil {
		return ErrAuthority
	}
	head, _ := json.Marshal(registryHead{0, hash(raw)})
	if publish(ctx, state, "authority.head.json", head) != nil {
		return ErrAuthority
	}
	parent, e := r.Open(".")
	if e != nil {
		return ErrAuthority
	}
	defer parent.Close()
	if parent.Sync() != nil {
		return ErrAuthority
	}
	return ctx.Err()
}
func OpenAuthority(ctx context.Context) (*Authority, error) {
	h, e := authoritativeHome()
	if e != nil {
		return nil, e
	}
	return openAuthorityAt(ctx, h)
}
func openAuthorityAt(ctx context.Context, home string) (*Authority, error) {
	r, e := authorityHome(home)
	if e != nil {
		return nil, e
	}
	fail := func() (*Authority, error) { r.Close(); return nil, ErrAuthority }
	sealBytes, e := readPrivate(ctx, r, enrollmentName, 2048)
	if e != nil {
		return fail()
	}
	var seal enrollment
	if decodeCanonical(sealBytes, &seal) != nil || seal.SchemaVersion != 1 || seal.Kind != "faultline-supervisor-enrollment" || !digest.MatchString(seal.Nonce) {
		return fail()
	}
	// Final state component must be a real private directory; root is pinned.
	st, e := r.Lstat(authorityDirectory)
	if e != nil || !st.IsDir() || st.Mode().Perm() != 0700 || !ownedDirectory(st) {
		return fail()
	}
	root, e := r.OpenRoot(authorityDirectory)
	if e != nil {
		return fail()
	}
	actual, e := root.Stat(".")
	if e != nil || !os.SameFile(st, actual) {
		root.Close()
		return fail()
	}
	a := &Authority{home: r, store: &JournalStore{root: root, save: publish}, seal: seal, probeRuntime: inspectRuntime}
	c, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	unlock, e := a.store.lock(c)
	if e != nil {
		a.Close()
		return nil, e
	}
	defer unlock()
	if _, _, e = a.registry(c); e != nil {
		a.Close()
		return nil, e
	}
	return a, nil
}
func (a *Authority) Close() error {
	a.closed.Store(true)
	e := a.store.Close()
	return errors.Join(e, a.home.Close())
}
func registryName(n int) string { return fmt.Sprintf("registry.%03d.json", n) }
func readPrivate(ctx context.Context, r *os.Root, name string, max int) ([]byte, error) {
	st, e := r.Lstat(name)
	if e != nil || !privateRegular(st) || st.Size() > int64(max) {
		return nil, ErrAuthority
	}
	f, e := openNoFollow(r, name)
	if e != nil {
		return nil, ErrAuthority
	}
	defer f.Close()
	now, e := f.Stat()
	if e != nil || !privateRegular(now) || !os.SameFile(st, now) {
		return nil, ErrAuthority
	}
	b, e := io.ReadAll(io.LimitReader(ctxReader{ctx, f}, int64(max)+1))
	if e != nil || len(b) > max {
		return nil, ErrAuthority
	}
	return b, nil
}
func decodeCanonical(b []byte, v any) error {
	if jsonstrict.Validate(b) != nil || json.Unmarshal(b, v) != nil {
		return ErrAuthority
	}
	canonical, e := json.Marshal(v)
	var compact bytes.Buffer
	if e != nil || json.Compact(&compact, b) != nil || !bytes.Equal(canonical, compact.Bytes()) {
		return ErrAuthority
	}
	return nil
}
func (a *Authority) registry(ctx context.Context) (registryHead, map[string]reservation, error) {
	bad := func() (registryHead, map[string]reservation, error) { return registryHead{}, nil, ErrAuthority }
	if a.closed.Load() {
		return bad()
	}
	named, e := a.home.Lstat(authorityDirectory)
	pinned, err := a.store.root.Stat(".")
	if e != nil || err != nil || !named.IsDir() || !os.SameFile(named, pinned) || named.Mode().Perm() != 0700 || !ownedDirectory(named) {
		return bad()
	}
	// Every access rechecks both enrollment anchors; missing state never initializes.
	for _, v := range []struct {
		r *os.Root
		n string
	}{{a.home, enrollmentName}, {a.store.root, "authority.json"}} {
		b, e := readPrivate(ctx, v.r, v.n, 2048)
		var s enrollment
		if e != nil || decodeCanonical(b, &s) != nil || s != a.seal {
			return bad()
		}
	}
	b, e := readPrivate(ctx, a.store.root, "authority.head.json", 2048)
	var head registryHead
	if e != nil || decodeCanonical(b, &head) != nil || head.Sequence < 0 || head.Sequence > 2*MaxAuthorityRuns || !digest.MatchString(head.SHA256) {
		return bad()
	}
	records := map[string]reservation{}
	previous := ""
	runs := 0
	for n := 0; n <= head.Sequence; n++ {
		raw, e := readPrivate(ctx, a.store.root, registryName(n), 2048)
		var rec registryRecord
		if e != nil || decodeCanonical(raw, &rec) != nil || rec.Sequence != n || rec.Previous != previous || rec.Authority != a.seal.Nonce {
			return bad()
		}
		previous = hash(raw)
		if n == 0 {
			if rec.Action != "genesis" || rec.LaunchID != "" || rec.PlanSHA256 != "" || rec.RuntimeKey != "" || rec.JournalSHA256 != "" {
				return bad()
			}
			continue
		}
		if !launchID.MatchString(rec.LaunchID) || !digest.MatchString(rec.PlanSHA256) || !digest.MatchString(rec.RuntimeKey) {
			return bad()
		}
		old, exists := records[rec.LaunchID]
		switch rec.Action {
		case "reserve", "import":
			if exists || rec.JournalSHA256 != "" {
				return bad()
			}
			runs++
			records[rec.LaunchID] = reservation{rec, false, rec.Action == "import"}
		case "release":
			if !exists || old.closed || old.record.PlanSHA256 != rec.PlanSHA256 || old.record.RuntimeKey != rec.RuntimeKey || !digest.MatchString(rec.JournalSHA256) {
				return bad()
			}
			old.closed = true
			old.record.JournalSHA256 = rec.JournalSHA256
			records[rec.LaunchID] = old
		default:
			return bad()
		}
	}
	if previous != head.SHA256 || runs > MaxAuthorityRuns {
		return bad()
	}
	// A record beyond the checkpoint is publication uncertainty, never ignored.
	for n := head.Sequence + 1; n <= 2*MaxAuthorityRuns; n++ {
		if _, e = a.store.root.Lstat(registryName(n)); !errors.Is(e, os.ErrNotExist) {
			return bad()
		}
	}
	d, e := a.store.root.Open(".")
	if e != nil {
		return bad()
	}
	defer d.Close()
	total := 0
	for {
		names, e := d.Readdirnames(64)
		total += len(names)
		if total > 4096 {
			return bad()
		}
		for _, name := range names {
			if strings.HasPrefix(name, "registry.") {
				valid := false
				for n := 0; n <= head.Sequence; n++ {
					if name == registryName(n) {
						valid = true
					}
				}
				if !valid {
					return bad()
				}
			}
			if strings.HasSuffix(name, ".journal.json") {
				if len(name) < len("faultline-lab-")+32 {
					return bad()
				}
				id := name[:len("faultline-lab-")+32]
				if _, ok := records[id]; !ok {
					return bad()
				}
			}
		}
		if e == io.EOF {
			break
		}
		if e != nil {
			return bad()
		}
		if ctx.Err() != nil {
			return bad()
		}
	}
	return head, records, nil
}

// The sole recognized mutable checkpoint is replaced under the stable guard.
// An interrupted checkpoint update fails closed; it never rolls history back.
func (a *Authority) checkpoint(ctx context.Context, head registryHead) error {
	old, e := readPrivate(ctx, a.store.root, "authority.head.json", 2048)
	if e != nil {
		return e
	}
	st, e := a.store.root.Lstat("authority.head.json")
	if e != nil {
		return ErrAuthority
	}
	var token [16]byte
	if _, e = rand.Read(token[:]); e != nil {
		return ErrAuthority
	}
	name := ".authority-head-" + hex.EncodeToString(token[:]) + ".tmp"
	b, _ := json.Marshal(head)
	if publish(ctx, a.store.root, name, b) != nil {
		return ErrAuthority
	}
	// No overwrite of a substituted object, symlink or different checkpoint.
	now, e := a.store.root.Lstat("authority.head.json")
	check, err := readPrivate(ctx, a.store.root, "authority.head.json", 2048)
	if e != nil || err != nil || !os.SameFile(st, now) || !bytes.Equal(old, check) || ctx.Err() != nil {
		return ErrAuthority
	}
	if a.store.root.Rename(name, "authority.head.json") != nil {
		return ErrAuthority
	}
	d, e := a.store.root.Open(".")
	if e != nil {
		return ErrAuthority
	}
	defer d.Close()
	if d.Sync() != nil {
		return ErrAuthority
	}
	return ctx.Err()
}
func (a *Authority) appendRegistry(ctx context.Context, h registryHead, r registryRecord) error {
	if h.Sequence >= 2*MaxAuthorityRuns {
		return ErrAuthority
	}
	r.Sequence = h.Sequence + 1
	r.Previous = h.SHA256
	r.Authority = a.seal.Nonce
	b, _ := json.Marshal(r)
	if publish(ctx, a.store.root, registryName(r.Sequence), b) != nil {
		return ErrAuthority
	}
	return a.checkpoint(ctx, registryHead{r.Sequence, hash(b)})
}

type TrustedLease struct {
	mu                   sync.Mutex
	authority            *Authority
	journal              Journal
	witness              runtimeWitness
	unlock               func()
	cleanupOnly          bool
	closed               bool
	recordingUnconfirmed bool
	imported             bool
	transportAttempts    map[string]*attemptState
	volatileCleanup      *Journal
	confirmedAbsent      map[ResourceKey]Proof
}

// Begin reserves/consumes only. No Docker mutations or isolation qualification.
func (a *Authority) Begin(ctx context.Context, p Plan, authorizedHash string) (*TrustedLease, error) {
	if authorizedHash != p.AuthorizationSHA256 {
		return nil, ErrAuthorization
	}
	w, e := inspectRuntime(ctx, p)
	if e != nil {
		return nil, e
	}
	return a.begin(ctx, p, w, false)
}
func (a *Authority) RecoverCleanup(ctx context.Context, p Plan, authorizedHash string) (*TrustedLease, error) {
	if authorizedHash != p.AuthorizationSHA256 {
		return nil, ErrAuthorization
	}
	w, e := inspectRuntime(ctx, p)
	if e != nil {
		return nil, e
	}
	return a.begin(ctx, p, w, true)
}
func (a *Authority) begin(ctx context.Context, p Plan, w runtimeWitness, cleanup bool) (*TrustedLease, error) {
	return a.beginMode(ctx, p, w, cleanup, false)
}
func (a *Authority) beginMode(ctx context.Context, p Plan, w runtimeWitness, cleanup, importing bool) (*TrustedLease, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if w.authorization.validate(p) != nil || !digest.MatchString(w.key) {
		return nil, ErrAuthorization
	}
	unlock, e := a.store.lock(ctx)
	if e != nil {
		return nil, e
	}
	defer unlock()
	h, records, e := a.registry(ctx)
	if e != nil {
		return nil, e
	}
	old, exists := records[p.Identity.LaunchID]
	if exists && (!cleanup || old.record.PlanSHA256 != p.AuthorizationSHA256 || old.record.RuntimeKey != w.key || old.closed || old.imported) {
		return nil, ErrReplay
	}
	if cleanup && !exists {
		return nil, ErrAuthority
	}
	for id, r := range records {
		j, e := a.store.load(ctx, id)
		if e != nil || j.Plan.AuthorizationSHA256 != r.record.PlanSHA256 {
			return nil, ErrAuthority
		}
		if r.closed {
			hash, _ := j.Hash()
			if hash != r.record.JournalSHA256 || !settled(j) {
				return nil, ErrAuthority
			}
			raw, err := readPrivate(ctx, a.store.root, id+".receipt.json", MaxJournalBytes)
			expected, _ := j.Receipt(authorizationFor(j.Plan))
			var actual CleanupReceipt
			if err != nil || decodeCanonical(raw, &actual) != nil || actual != expected {
				return nil, ErrAuthority
			}
		} else if id != p.Identity.LaunchID || !cleanup {
			return nil, ErrLease
		}
	}
	if !cleanup && len(records) >= MaxAuthorityRuns {
		return nil, ErrAuthority
	}
	processUnlock, e := lockNamed(ctx, a.store.root, "runtime-"+w.key+".guard")
	if e != nil {
		return nil, e
	}
	success := false
	defer func() {
		if !success {
			processUnlock()
		}
	}()
	var j Journal
	if cleanup {
		j, e = a.store.load(ctx, p.Identity.LaunchID)
	} else {
		// Durable reservation precedes journal creation. Any partial failure leaves a
		// blocking reservation; absence of a journal cannot become fresh authority.
		action := "reserve"
		if importing {
			action = "import"
		}
		if e = a.appendRegistry(ctx, h, registryRecord{Action: action, LaunchID: p.Identity.LaunchID, PlanSHA256: p.AuthorizationSHA256, RuntimeKey: w.key}); e != nil {
			return nil, e
		}
		j, e = NewJournal(p, w.authorization, nil)
		if e == nil {
			var b []byte
			b, e = j.Encode()
			if e == nil {
				e = a.store.save(ctx, a.store.root, journalName(p.Identity.LaunchID, 0), b)
			}
		}
	}
	if e != nil || ctx.Err() != nil {
		return nil, ErrAuthority
	}
	success = true
	return &TrustedLease{authority: a, journal: j, witness: w, unlock: processUnlock, cleanupOnly: cleanup, imported: importing}, nil
}
func settled(j Journal) bool {
	s, e := j.State()
	return e == nil && s.LabCleanup == "complete" && (s.ProxyCleanup == "complete" || s.ProxyCleanup == "not_created") && (s.Worker == "exited" || s.Worker == "not_started")
}
func (l *TrustedLease) Close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.closed {
		l.closed = true
		l.unlock()
	}
} // deliberately NOT a durable release
func (l *TrustedLease) Append(ctx context.Context, event Event) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed || l.authority == nil {
		return ErrLease
	}
	if l.imported {
		return ErrAuthority
	}
	switch event.Operation {
	case Created, CreateAbsent, WorkerRunning, WorkerExited, ProxyAbsent, DeleteAbsent:
		return ErrObservation
	}
	if (l.cleanupOnly || l.recordingUnconfirmed) && !cleanupOperation(event.Operation) {
		return ErrAuthorization
	}
	j, e := l.authority.store.Append(ctx, l.journal, l.witness.authorization, event)
	if e != nil {
		l.recordingUnconfirmed = true
		return e
	}
	l.journal = j
	return nil
}
func (l *TrustedLease) ConfirmCleanup(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.confirmCleanupLocked(ctx, nil)
}
func (l *TrustedLease) confirmCleanupLocked(ctx context.Context, verifier *certifier) error {
	if l.closed || l.authority == nil || l.imported || !settled(l.journal) {
		return ErrAuthority
	}
	attempted := false
	for _, event := range l.journal.Events {
		if event.Operation == CreateIntent {
			attempted = true
		}
	}
	if attempted {
		if verifier == nil {
			system, e := systemCertifier()
			if e != nil {
				return e
			}
			verifier = &system
		}
		if e := l.verifyFinalAbsence(ctx, *verifier); e != nil {
			return e
		}
	}
	if e := l.checkRuntime(ctx); e != nil {
		return e
	}
	// Receipt is durable before release; an error never releases the reservation.
	if e := l.authority.store.WriteReceipt(ctx, l.journal, l.witness.authorization); e != nil {
		return e
	}
	c, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	unlock, e := l.authority.store.lock(c)
	if e != nil {
		return e
	}
	defer unlock()
	h, records, e := l.authority.registry(c)
	if e != nil {
		return e
	}
	r, ok := records[l.journal.Plan.Identity.LaunchID]
	if !ok || r.closed || r.record.RuntimeKey != l.witness.key {
		return ErrAuthority
	}
	current, e := l.authority.store.load(c, l.journal.Plan.Identity.LaunchID)
	if e != nil || !journalsEqual(current, l.journal) {
		return ErrAuthority
	}
	jh, _ := current.Hash()
	return l.authority.appendRegistry(c, h, registryRecord{Action: "release", LaunchID: current.Plan.Identity.LaunchID, PlanSHA256: current.Plan.AuthorizationSHA256, RuntimeKey: l.witness.key, JournalSHA256: jh})
}

// ImportConsumed preserves an explicitly supplied, fully validated old journal
// as a blocking tombstone. It never adopts resources, scans paths or enables RUN.
// Partial import leaves a blocking reservation for separate reviewed recovery.
func (a *Authority) ImportConsumed(ctx context.Context, j Journal, authorizedHash string) error {
	if j.Validate() != nil || authorizedHash != j.Plan.AuthorizationSHA256 {
		return ErrAuthorization
	}
	w, e := inspectRuntime(ctx, j.Plan)
	if e != nil {
		return e
	}
	lease, e := a.importVerified(ctx, j, w)
	if lease != nil {
		lease.Close()
	}
	return e
}
func (a *Authority) importVerified(ctx context.Context, j Journal, w runtimeWitness) (*TrustedLease, error) {
	if j.Validate() != nil {
		return nil, ErrJournal
	}
	l, e := a.beginMode(ctx, j.Plan, w, false, true)
	if e != nil {
		return nil, e
	}
	l.cleanupOnly = true
	success := false
	defer func() {
		if !success {
			l.Close()
		}
	}()
	c, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	unlock, e := a.store.lock(c)
	if e != nil {
		return nil, e
	}
	defer unlock()
	for n := 1; n <= len(j.Events); n++ {
		prefix := j
		prefix.Events = append([]Event{}, j.Events[:n]...)
		b, e := prefix.Encode()
		if e != nil {
			return nil, e
		}
		if a.store.save(c, a.store.root, journalName(j.Plan.Identity.LaunchID, n), b) != nil {
			return nil, ErrAuthority
		}
		l.journal = prefix
	}
	success = true
	return l, nil
}

func (l *TrustedLease) checkRuntime(ctx context.Context) error {
	if l.authority == nil || l.authority.probeRuntime == nil {
		return ErrAuthority
	}
	current, e := l.authority.probeRuntime(ctx, l.journal.Plan)
	if e != nil {
		return e
	}
	if current != l.witness {
		return ErrAuthorization
	}
	return nil
}
