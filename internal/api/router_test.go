package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/kc1wfotfm4/qry-schema-registry/internal/store"
)

func TestHealthzReportsOK(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "service.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	NewRouter(st).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if got := recorder.Body.String(); got != `{"database":"ok","status":"ok"}` {
		t.Fatalf("body = %s", got)
	}
}

func TestUnknownRouteUsesPublishedErrorShape(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "service.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/missing", nil)
	NewRouter(st).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
}

func newTestRouter(t *testing.T) http.Handler {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "service.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return NewRouter(st)
}

func postVersion(t *testing.T, router http.Handler, subject, body string) (int, map[string]any) {
	t.Helper()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/subjects/"+subject+"/versions", strings.NewReader(body))
	router.ServeHTTP(recorder, request)
	var decoded map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("response is not JSON: %q", recorder.Body.String())
	}
	return recorder.Code, decoded
}

func errorCode(t *testing.T, body map[string]any) string {
	t.Helper()
	errObj, ok := body["error"].(map[string]any)
	if !ok {
		t.Fatalf("body %v has no top-level error object", body)
	}
	code, _ := errObj["code"].(string)
	message, _ := errObj["message"].(string)
	if code == "" || message == "" {
		t.Fatalf("error object %v must carry non-empty code and message", errObj)
	}
	if len(body) != 1 {
		t.Fatalf("error body %v must only carry the error object", body)
	}
	return code
}

func TestRegisterVersionCreated(t *testing.T) {
	router := newTestRouter(t)
	schemaText := `{"fields":{"id":"string"},"required":["id"]}`
	body := fmt.Sprintf(`{"schema":%q,"compatibility":"BACKWARD"}`, schemaText)

	status, decoded := postVersion(t, router, "user-events", body)
	if status != http.StatusCreated {
		t.Fatalf("status = %d, want %d (%v)", status, http.StatusCreated, decoded)
	}
	want := map[string]any{
		"subject":       "user-events",
		"schema":        schemaText,
		"version":       float64(1),
		"compatibility": "BACKWARD",
	}
	for key, wantValue := range want {
		if decoded[key] != wantValue {
			t.Fatalf("body[%q] = %v, want %v (body %v)", key, decoded[key], wantValue, decoded)
		}
	}
}

func TestRegisterVersionNumbersAreSequentialPerSubject(t *testing.T) {
	router := newTestRouter(t)
	register := func(subject string) float64 {
		t.Helper()
		status, decoded := postVersion(t, router, subject, `{"schema":"{\"fields\":{},\"required\":[]}","compatibility":"NONE"}`)
		if status != http.StatusCreated {
			t.Fatalf("status = %d, want %d (%v)", status, http.StatusCreated, decoded)
		}
		return decoded["version"].(float64)
	}
	for want := float64(1); want <= 3; want++ {
		if got := register("alpha"); got != want {
			t.Fatalf("alpha version = %v, want %v", got, want)
		}
	}
	if got := register("beta"); got != 1 {
		t.Fatalf("beta version = %v, want 1", got)
	}
}

func TestRegisterVersionRejectsMalformedJSON(t *testing.T) {
	router := newTestRouter(t)
	for _, body := range []string{`{`, `not json`, `[1,2]`, `"text"`, `null`, ``} {
		status, decoded := postVersion(t, router, "alpha", body)
		if status != http.StatusBadRequest {
			t.Fatalf("body %q: status = %d, want %d", body, status, http.StatusBadRequest)
		}
		if code := errorCode(t, decoded); code != "invalid_request" {
			t.Fatalf("body %q: code = %q, want invalid_request", body, code)
		}
	}
}

func TestRegisterVersionRejectsMissingFields(t *testing.T) {
	router := newTestRouter(t)
	bodies := []string{
		`{"compatibility":"NONE"}`,
		`{"schema":"{\"fields\":{},\"required\":[]}"}`,
		`{}`,
	}
	for _, body := range bodies {
		status, decoded := postVersion(t, router, "alpha", body)
		if status != http.StatusBadRequest {
			t.Fatalf("body %q: status = %d, want %d", body, status, http.StatusBadRequest)
		}
		if code := errorCode(t, decoded); code != "missing_field" {
			t.Fatalf("body %q: code = %q, want missing_field", body, code)
		}
	}
}

func TestRegisterVersionRejectsInvalidSchema(t *testing.T) {
	router := newTestRouter(t)
	schemas := []string{
		`not json`,
		`{"required":[]}`,
		`{"fields":{"id":1},"required":[]}`,
		`{"fields":{"id":"string"},"required":["missing"]}`,
		`{"fields":{"id":"string"}}`,
	}
	for _, schemaText := range schemas {
		body := fmt.Sprintf(`{"schema":%q,"compatibility":"NONE"}`, schemaText)
		status, decoded := postVersion(t, router, "alpha", body)
		if status != http.StatusBadRequest {
			t.Fatalf("schema %q: status = %d, want %d", schemaText, status, http.StatusBadRequest)
		}
		if code := errorCode(t, decoded); code != "invalid_schema" {
			t.Fatalf("schema %q: code = %q, want invalid_schema", schemaText, code)
		}
	}

	// schema must be a JSON string in the request body.
	status, decoded := postVersion(t, router, "alpha", `{"schema":{"fields":{}},"compatibility":"NONE"}`)
	if status != http.StatusBadRequest {
		t.Fatalf("non-string schema: status = %d, want %d", status, http.StatusBadRequest)
	}
	if code := errorCode(t, decoded); code != "invalid_schema" {
		t.Fatalf("non-string schema: code = %q, want invalid_schema", code)
	}
}

func TestRegisterVersionRejectsInvalidCompatibility(t *testing.T) {
	router := newTestRouter(t)
	for _, compatibility := range []string{`"backward"`, `"TRANSITIVE"`, `""`, `42`, `null`} {
		body := `{"schema":"{\"fields\":{},\"required\":[]}","compatibility":` + compatibility + `}`
		status, decoded := postVersion(t, router, "alpha", body)
		if status != http.StatusBadRequest {
			t.Fatalf("compatibility %s: status = %d, want %d", compatibility, status, http.StatusBadRequest)
		}
		if code := errorCode(t, decoded); code != "invalid_compatibility" {
			t.Fatalf("compatibility %s: code = %q, want invalid_compatibility", compatibility, code)
		}
	}
}

func TestRegisterVersionEnforcesCompatibility(t *testing.T) {
	router := newTestRouter(t)
	base := `{"fields":{"id":"string","age":"int"},"required":["id"]}`
	body := fmt.Sprintf(`{"schema":%q,"compatibility":"FULL"}`, base)
	if status, decoded := postVersion(t, router, "alpha", body); status != http.StatusCreated {
		t.Fatalf("seed: status = %d (%v)", status, decoded)
	}

	// FULL forbids removing a field.
	dropped := `{"fields":{"id":"string"},"required":["id"]}`
	status, decoded := postVersion(t, router, "alpha", fmt.Sprintf(`{"schema":%q,"compatibility":"FULL"}`, dropped))
	if status != http.StatusConflict {
		t.Fatalf("status = %d, want %d (%v)", status, http.StatusConflict, decoded)
	}
	if code := errorCode(t, decoded); code != "incompatible_schema" {
		t.Fatalf("code = %q, want incompatible_schema", code)
	}

	// The conflict must not write a new version or change the old one: the next
	// valid registration is still checked against version 1 and numbered 2.
	extended := `{"fields":{"id":"string","age":"int","email":"string"},"required":["id"]}`
	status, decoded = postVersion(t, router, "alpha", fmt.Sprintf(`{"schema":%q,"compatibility":"FULL"}`, extended))
	if status != http.StatusCreated {
		t.Fatalf("status = %d, want %d (%v)", status, http.StatusCreated, decoded)
	}
	if got := decoded["version"].(float64); got != 2 {
		t.Fatalf("version = %v, want 2", got)
	}
}

func TestRegisterVersionNoneSkipsCompatibilityCheck(t *testing.T) {
	router := newTestRouter(t)
	first := `{"fields":{"id":"string"},"required":["id"]}`
	if status, decoded := postVersion(t, router, "alpha", fmt.Sprintf(`{"schema":%q,"compatibility":"NONE"}`, first)); status != http.StatusCreated {
		t.Fatalf("seed: status = %d (%v)", status, decoded)
	}
	// NONE performs no checks, so even a breaking change is accepted.
	second := `{"fields":{},"required":[]}`
	status, decoded := postVersion(t, router, "alpha", fmt.Sprintf(`{"schema":%q,"compatibility":"NONE"}`, second))
	if status != http.StatusCreated {
		t.Fatalf("status = %d, want %d (%v)", status, http.StatusCreated, decoded)
	}
	if got := decoded["version"].(float64); got != 2 {
		t.Fatalf("version = %v, want 2", got)
	}
}

func TestRegisterVersionConcurrentRegistrationsStayContiguous(t *testing.T) {
	router := newTestRouter(t)

	const callers = 24
	versions := make(chan float64, callers)
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			status, decoded := postVersion(t, router, "alpha", `{"schema":"{\"fields\":{},\"required\":[]}","compatibility":"NONE"}`)
			if status != http.StatusCreated {
				t.Errorf("status = %d, want %d (%v)", status, http.StatusCreated, decoded)
				return
			}
			versions <- decoded["version"].(float64)
		}()
	}
	wg.Wait()
	close(versions)

	seen := make(map[float64]bool, callers)
	for version := range versions {
		if seen[version] {
			t.Fatalf("version %v allocated twice", version)
		}
		seen[version] = true
	}
	if len(seen) != callers {
		t.Fatalf("allocated %d distinct versions, want %d", len(seen), callers)
	}
	for want := float64(1); want <= callers; want++ {
		if !seen[want] {
			t.Fatalf("version %v missing from allocations %v", want, seen)
		}
	}
}
