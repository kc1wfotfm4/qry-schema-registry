package store

import (
	"errors"
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

func TestRegisterVersionAssignsConsecutiveVersionsPerSubject(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "store.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()

	for want := int64(1); want <= 3; want++ {
		got, err := st.RegisterVersion(7, `{"fields":{},"required":[]}`, "NONE", nil)
		if err != nil {
			t.Fatalf("register: %v", err)
		}
		if got != want {
			t.Fatalf("version = %d, want %d", got, want)
		}
	}

	other, err := st.RegisterVersion(8, `{"fields":{},"required":[]}`, "NONE", nil)
	if err != nil {
		t.Fatalf("register other subject: %v", err)
	}
	if other != 1 {
		t.Fatalf("other subject version = %d, want 1", other)
	}
}

func TestRegisterVersionRejectionLeavesNoGap(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "store.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()

	if _, err := st.RegisterVersion(1, `{"fields":{"a":"string"},"required":[]}`, "NONE", nil); err != nil {
		t.Fatalf("register first: %v", err)
	}

	reject := errors.New("reject")
	validate := func(previous string) error {
		if previous != `{"fields":{"a":"string"},"required":[]}` {
			t.Fatalf("previous = %q", previous)
		}
		return reject
	}
	if _, err := st.RegisterVersion(1, `{"fields":{},"required":[]}`, "FULL", validate); !errors.Is(err, reject) {
		t.Fatalf("rejected register err = %v, want %v", err, reject)
	}

	got, err := st.RegisterVersion(1, `{"fields":{"a":"string"},"required":[]}`, "FULL", nil)
	if err != nil {
		t.Fatalf("register after rejection: %v", err)
	}
	if got != 2 {
		t.Fatalf("version after rejection = %d, want 2", got)
	}
}

func TestRegisterVersionConcurrentRegistrationsStayDense(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "store.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()

	const workers = 16
	versions := make(chan int64, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			version, err := st.RegisterVersion(42, `{"fields":{},"required":[]}`, "NONE", nil)
			if err != nil {
				t.Errorf("register: %v", err)
				return
			}
			versions <- version
		}()
	}
	wg.Wait()
	close(versions)

	seen := make(map[int64]bool, workers)
	for version := range versions {
		if seen[version] {
			t.Fatalf("duplicate version %d", version)
		}
		seen[version] = true
	}
	for want := int64(1); want <= workers; want++ {
		if !seen[want] {
			t.Fatalf("missing version %d in %v", want, seen)
		}
	}
}

func TestListSubjectsEmptyStore(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "store.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()

	subjects, err := st.ListSubjects()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if subjects == nil || len(subjects) != 0 {
		t.Fatalf("subjects = %v, want empty non-nil slice", subjects)
	}
}

func TestListSubjectsSortedAndUnique(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "store.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()

	for _, subject := range []int64{9, 3, 9, 12, 3} {
		if _, err := st.RegisterVersion(subject, `{"fields":{},"required":[]}`, "NONE", nil); err != nil {
			t.Fatalf("register subject %d: %v", subject, err)
		}
	}

	subjects, err := st.ListSubjects()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	want := []int64{3, 9, 12}
	if len(subjects) != len(want) {
		t.Fatalf("subjects = %v, want %v", subjects, want)
	}
	for i := range want {
		if subjects[i] != want[i] {
			t.Fatalf("subjects = %v, want %v", subjects, want)
		}
	}
}

func TestGetVersionReturnsStoredRecord(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "store.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()

	raw := `{"fields":{"id":"string"},"required":["id"]}`
	if _, err := st.RegisterVersion(4, raw, "FULL", nil); err != nil {
		t.Fatalf("register: %v", err)
	}
	if _, err := st.RegisterVersion(4, `{"fields":{},"required":[]}`, "NONE", nil); err != nil {
		t.Fatalf("register: %v", err)
	}

	record, err := st.GetVersion(4, 1)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if record.Subject != 4 || record.Version != 1 {
		t.Fatalf("record = %+v, want subject 4 version 1", record)
	}
	if record.Schema != raw {
		t.Fatalf("schema = %q, want %q", record.Schema, raw)
	}
	if record.Compatibility != "FULL" {
		t.Fatalf("compatibility = %q, want FULL", record.Compatibility)
	}
}

func TestGetVersionDistinguishesMissingSubjectAndVersion(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "store.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()

	if _, err := st.GetVersion(1, 1); !errors.Is(err, ErrSubjectNotFound) {
		t.Fatalf("empty store err = %v, want %v", err, ErrSubjectNotFound)
	}

	if _, err := st.RegisterVersion(1, `{"fields":{},"required":[]}`, "NONE", nil); err != nil {
		t.Fatalf("register: %v", err)
	}
	if _, err := st.GetVersion(1, 2); !errors.Is(err, ErrVersionNotFound) {
		t.Fatalf("missing version err = %v, want %v", err, ErrVersionNotFound)
	}
	if _, err := st.GetVersion(2, 1); !errors.Is(err, ErrSubjectNotFound) {
		t.Fatalf("missing subject err = %v, want %v", err, ErrSubjectNotFound)
	}
}
