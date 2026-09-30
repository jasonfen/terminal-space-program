package sessiondir

import (
	"fmt"
	"sync"
	"testing"
)

// The CLI (promote/demote/invite) and a running server are separate
// processes, each with its own Store and its own in-process mutex. Two
// Stores on one dir model that. Without a cross-process lock a
// read-modify-write in one loses the other's write.
func TestCrossStoreReadModifyWriteLosesNoUpdate(t *testing.T) {
	dir := t.TempDir()
	server, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	cli, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := server.EnsureHost("jason"); err != nil {
		t.Fatal(err)
	}
	inv, err := server.MintInvite("dave")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := server.Enroll(inv.Code, "SHA256:dave", "dave"); err != nil {
		t.Fatal(err)
	}

	const n = 150
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { // the running server minting invites
		defer wg.Done()
		for i := 0; i < n; i++ {
			if _, err := server.MintInvite(fmt.Sprintf("guest%d", i)); err != nil {
				t.Error(err)
				return
			}
		}
	}()
	go func() { // the CLI flipping a role
		defer wg.Done()
		for i := 0; i < n; i++ {
			var err error
			if i%2 == 0 {
				err = cli.PromoteAdmin("SHA256:dave")
			} else {
				err = cli.DemoteAdmin("SHA256:dave")
			}
			if err != nil {
				t.Error(err)
				return
			}
		}
	}()
	wg.Wait()

	m, err := server.Meta()
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Invites) != n {
		t.Errorf("invites = %d, want %d: a concurrent write was lost", len(m.Invites), n)
	}
}

func TestOpenReadOnlyWritesNothing(t *testing.T) {
	dir := t.TempDir() + "/missing"
	if _, err := OpenReadOnly(dir); err == nil {
		t.Fatal("OpenReadOnly on a missing dir should error")
	}
	if _, err := osStat(dir); err == nil {
		t.Fatal("OpenReadOnly created the directory")
	}

	real := t.TempDir()
	s, err := Open(real)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.EnsureHost("jason"); err != nil {
		t.Fatal(err)
	}
	before := snapshotDir(t, real)
	ro, err := OpenReadOnly(real)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ro.Meta(); err != nil {
		t.Fatal(err)
	}
	if after := snapshotDir(t, real); after != before {
		t.Errorf("read-only open changed the dir:\nbefore %s\nafter  %s", before, after)
	}
}
