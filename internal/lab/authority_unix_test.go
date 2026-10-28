//go:build darwin || linux

package lab

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
)

func TestAuthoritySubstitutionFailsClosed(t *testing.T) {
	for _, kind := range []string{"symlink", "fifo", "hardlink", "permissions"} {
		t.Run(kind, func(t *testing.T) {
			a, home := authorityFixture(t)
			path := filepath.Join(home, authorityDirectory, "authority.head.json")
			if e := os.Remove(path); e != nil {
				t.Fatal(e)
			}
			switch kind {
			case "symlink":
				if e := os.Symlink("authority.json", path); e != nil {
					t.Fatal(e)
				}
			case "fifo":
				if e := syscall.Mkfifo(path, 0600); e != nil {
					t.Fatal(e)
				}
			case "hardlink":
				if e := os.Link(filepath.Join(home, authorityDirectory, "authority.json"), path); e != nil {
					t.Fatal(e)
				}
			case "permissions":
				if e := os.WriteFile(path, []byte(`{}`), 0644); e != nil {
					t.Fatal(e)
				}
			}
			if _, _, e := a.registry(context.Background()); !errors.Is(e, ErrAuthority) {
				t.Fatal(e)
			}
		})
	}
}
func TestAuthorityProcessExitRetainsDurableReservation(t *testing.T) {
	if os.Getenv("FAULTLINE_AUTHORITY_CHILD") == "1" {
		home := os.Getenv("FAULTLINE_AUTHORITY_TEST_HOME")
		a, e := openAuthorityAt(context.Background(), home)
		if e != nil {
			os.Exit(41)
		}
		p := syntheticPlan(t)
		l, e := a.begin(context.Background(), p, fakeWitness(p), false)
		if e != nil {
			os.Exit(42)
		}
		if l.Append(context.Background(), Event{Operation: Prepare}) != nil {
			os.Exit(43)
		}
		os.Exit(9)
	}
	a, home := authorityFixture(t)
	cmd := exec.Command(os.Args[0], "-test.run=^TestAuthorityProcessExitRetainsDurableReservation$")
	cmd.Env = append(os.Environ(), "FAULTLINE_AUTHORITY_CHILD=1", "FAULTLINE_AUTHORITY_TEST_HOME="+home)
	if e := cmd.Run(); e == nil {
		t.Fatal("child did not exit")
	} else if x, ok := e.(*exec.ExitError); !ok || x.ExitCode() != 9 {
		t.Fatal(e)
	}
	p := randomPlan(t)
	if _, e := a.begin(context.Background(), p, fakeWitness(p), false); !errors.Is(e, ErrLease) {
		t.Fatal("process exit freed durable reservation", e)
	}
	original := syntheticPlan(t)
	cleanup, e := a.begin(context.Background(), original, fakeWitness(original), true)
	if e != nil {
		t.Fatal("kernel lock not released", e)
	}
	cleanup.Close()
}

func TestAuthorityRootAndAnchorComponentsCannotBeSubstituted(t *testing.T) {
	for _, kind := range []string{"root-symlink", "anchor-symlink", "root-permissions"} {
		t.Run(kind, func(t *testing.T) {
			a, home := authorityFixture(t)
			root := filepath.Join(home, authorityDirectory)
			switch kind {
			case "root-symlink":
				if e := os.Rename(root, root+".saved"); e != nil {
					t.Fatal(e)
				}
				if e := os.Symlink(authorityDirectory+".saved", root); e != nil {
					t.Fatal(e)
				}
			case "anchor-symlink":
				name := filepath.Join(home, enrollmentName)
				if e := os.Remove(name); e != nil {
					t.Fatal(e)
				}
				if e := os.Symlink(filepath.Join(authorityDirectory, "authority.json"), name); e != nil {
					t.Fatal(e)
				}
			case "root-permissions":
				if e := os.Chmod(root, 0755); e != nil {
					t.Fatal(e)
				}
			}
			if _, e := openAuthorityAt(context.Background(), home); e == nil {
				t.Fatal("substituted state opened")
			}
			if _, _, e := a.registry(context.Background()); e == nil {
				t.Fatal("pinned object concealed replacement")
			}
		})
	}
}
