package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/kc1wfotfm4/qry-schema-registry/internal/store"
)

func newTestRouter(t *testing.T) (*gin.Engine, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "service.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return NewRouter(st), st
}

func postVersion(t *testing.T, router *gin.Engine, subject string, body string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/subjects/"+subject+"/versions", strings.NewReader(body))
	router.ServeHTTP(recorder, request)
	return recorder
}

func decodeBody(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var decoded map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode %q: %v", recorder.Body.String(), err)
	}
	return decoded
}

func requireError(t *testing.T, recorder *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if recorder.Code != status {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, status, recorder.Body.String())
	}
	decoded := decodeBody(t, recorder)
	if len(decoded) != 1 {
		t.Fatalf("error body keys = %v, want only error", decoded)
	}
	errObj, ok := decoded["error"].(map[string]any)
	if !ok {
		t.Fatalf("error = %v, want object", decoded["error"])
	}
	if errObj["code"] != code {
		t.Fatalf("code = %v, want %v", errObj["code"], code)
	}
	if _, ok := errObj["message"].(string); !ok {
		t.Fatalf("message = %v, want string", errObj["message"])
	}
}

func TestHealthzReportsOK(t *testing.T) {
	router, _ := newTestRouter(t)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if got := recorder.Body.String(); got != `{"database":"ok","status":"ok"}` {
		t.Fatalf("body = %s", got)
	}
}

func TestUnknownRouteUsesPublishedErrorShape(t *testing.T) {
	router, _ := newTestRouter(t)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/missing", nil)
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
}

func TestRegisterVersionReturnsCreated(t *testing.T) {
	router, _ := newTestRouter(t)
	schemaText := `{"fields":{"id":"string"},"required":["id"]}`
	body := fmt.Sprintf(`{"schema":%q,"compatibility":"NONE"}`, schemaText)

	recorder := postVersion(t, router, "5", body)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusCreated, recorder.Body.String())
	}
	decoded := decodeBody(t, recorder)
	if decoded["subject"] != float64(5) {
		t.Fatalf("subject = %v, want 5", decoded["subject"])
	}
	if decoded["version"] != float64(1) {
		t.Fatalf("version = %v, want 1", decoded["version"])
	}
	if decoded["schema"] != schemaText {
		t.Fatalf("schema = %v, want %v", decoded["schema"], schemaText)
	}
	if decoded["compatibility"] != "NONE" {
		t.Fatalf("compatibility = %v, want NONE", decoded["compatibility"])
	}

	second := postVersion(t, router, "5", body)
	if got := decodeBody(t, second)["version"]; got != float64(2) {
		t.Fatalf("second version = %v, want 2", got)
	}
	other := postVersion(t, router, "6", body)
	if got := decodeBody(t, other)["version"]; got != float64(1) {
		t.Fatalf("other subject version = %v, want 1", got)
	}
}

func TestRegisterVersionRejectsMalformedRequests(t *testing.T) {
	router, _ := newTestRouter(t)
	valid := `{"schema":"{\"fields\":{\"id\":\"string\"},\"required\":[\"id\"]}","compatibility":"NONE"}`

	cases := []struct {
		name    string
		subject string
		body    string
		status  int
		code    string
	}{
		{"non numeric subject", "abc", valid, http.StatusBadRequest, "invalid_request"},
		{"empty body", "1", "", http.StatusBadRequest, "invalid_request"},
		{"not json", "1", `{oops`, http.StatusBadRequest, "invalid_request"},
		{"json array body", "1", `[]`, http.StatusBadRequest, "invalid_request"},
		{"missing both fields", "1", `{}`, http.StatusBadRequest, "missing_field"},
		{"missing compatibility", "1", `{"schema":"{}"}`, http.StatusBadRequest, "missing_field"},
		{"missing schema", "1", `{"compatibility":"NONE"}`, http.StatusBadRequest, "missing_field"},
		{"schema not a string", "1", `{"schema":{},"compatibility":"NONE"}`, http.StatusBadRequest, "invalid_schema"},
		{"schema not json", "1", `{"schema":"nope","compatibility":"NONE"}`, http.StatusBadRequest, "invalid_schema"},
		{"schema missing required", "1", `{"schema":"{\"fields\":{}}","compatibility":"NONE"}`, http.StatusBadRequest, "invalid_schema"},
		{"schema required not subset", "1", `{"schema":"{\"fields\":{},\"required\":[\"x\"]}","compatibility":"NONE"}`, http.StatusBadRequest, "invalid_schema"},
		{"compatibility not a string", "1", `{"schema":"{\"fields\":{},\"required\":[]}","compatibility":1}`, http.StatusBadRequest, "invalid_compatibility"},
		{"compatibility unknown", "1", `{"schema":"{\"fields\":{},\"required\":[]}","compatibility":"SIDEWAYS"}`, http.StatusBadRequest, "invalid_compatibility"},
		{"compatibility wrong case", "1", `{"schema":"{\"fields\":{},\"required\":[]}","compatibility":"none"}`, http.StatusBadRequest, "invalid_compatibility"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			requireError(t, postVersion(t, router, tc.subject, tc.body), tc.status, tc.code)
		})
	}
}

func TestRegisterVersionEnforcesCompatibility(t *testing.T) {
	router, _ := newTestRouter(t)
	first := `{"schema":"{\"fields\":{\"id\":\"string\",\"nick\":\"string\"},\"required\":[\"id\"]}","compatibility":"NONE"}`
	if recorder := postVersion(t, router, "9", first); recorder.Code != http.StatusCreated {
		t.Fatalf("seed status = %d (body %s)", recorder.Code, recorder.Body.String())
	}

	breaking := `{"schema":"{\"fields\":{\"nick\":\"string\"},\"required\":[]}","compatibility":"BACKWARD"}`
	requireError(t, postVersion(t, router, "9", breaking), http.StatusConflict, "incompatible_schema")

	allowed := `{"schema":"{\"fields\":{\"id\":\"string\",\"nick\":\"string\",\"email\":\"string\"},\"required\":[\"id\"]}","compatibility":"FULL"}`
	recorder := postVersion(t, router, "9", allowed)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusCreated, recorder.Body.String())
	}
	if got := decodeBody(t, recorder)["version"]; got != float64(2) {
		t.Fatalf("version = %v, want 2: rejected attempt must not consume a number", got)
	}
}
