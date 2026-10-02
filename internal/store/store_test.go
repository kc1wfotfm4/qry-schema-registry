package store

import (
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
)

func TestOpenCreatesUsableStore(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "store.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()
	if err := st.Ping(); err != nil {
		t.Fatalf("ping: %v", err)
	}
}

func TestRegisterVersionAssignsSequentialVersionsPerSubject(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "store.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()

	for want := 1; want <= 3; want++ {
		version, err := st.RegisterVersion("alpha", fmt.Sprintf(`{"v":%d}`, want), "NONE", nil)
		if err != nil {
			t.Fatalf("register alpha: %v", err)
		}
		if version.Version != want {
			t.Fatalf("alpha version = %d, want %d", version.Version, want)
		}
		if version.Subject != "alpha" || version.Schema == "" || version.Compatibility != "NONE" {
			t.Fatalf("unexpected stored version: %+v", version)
		}
	}

	version, err := st.RegisterVersion("beta", `{"v":1}`, "FULL", nil)
	if err != nil {
		t.Fatalf("register beta: %v", err)
	}
	if version.Version != 1 {
		t.Fatalf("beta version = %d, want 1", version.Version)
	}
}

func TestRegisterVersionCheckSeesPreviousAndAbortsCleanly(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "store.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()

	if _, err := st.RegisterVersion("alpha", `{"v":1}`, "BACKWARD", nil); err != nil {
		t.Fatalf("register first: %v", err)
	}

	reject := errors.New("reject")
	var seen *Version
	_, err = st.RegisterVersion("alpha", `{"v":2}`, "FULL", func(prev *Version) error {
		seen = prev
		return reject
	})
	if !errors.Is(err, reject) {
		t.Fatalf("register error = %v, want %v", err, reject)
	}
	if seen == nil || seen.Version != 1 || seen.Schema != `{"v":1}` || seen.Compatibility != "BACKWARD" {
		t.Fatalf("check observed %+v, want version 1 of alpha", seen)
	}

	// The aborted registration must not consume a version number.
	version, err := st.RegisterVersion("alpha", `{"v":2}`, "FULL", nil)
	if err != nil {
		t.Fatalf("register after abort: %v", err)
	}
	if version.Version != 2 {
		t.Fatalf("version after abort = %d, want 2", version.Version)
	}
}

func TestRegisterVersionConcurrentAllocatesUniqueContiguousVersions(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "store.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()

	const callers = 32
	versions := make(chan int, callers)
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			version, err := st.RegisterVersion("alpha", fmt.Sprintf(`{"v":%d}`, n), "NONE", nil)
			if err != nil {
				t.Errorf("register: %v", err)
				return
			}
			versions <- version.Version
		}(i)
	}
	wg.Wait()
	close(versions)

	seen := make(map[int]bool, callers)
	for version := range versions {
		if version < 1 || version > callers {
			t.Fatalf("version %d out of range [1,%d]", version, callers)
		}
		if seen[version] {
			t.Fatalf("version %d allocated twice", version)
		}
		seen[version] = true
	}
	if len(seen) != callers {
		t.Fatalf("allocated %d distinct versions, want %d", len(seen), callers)
	}
}
