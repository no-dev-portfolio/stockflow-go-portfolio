package httpapi_test

import (
	"bytes"
	"encoding/json"
	"example.com/stockflow/internal/httpapi"
	"example.com/stockflow/internal/testutil"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func handler(t *testing.T) http.Handler {
	db := testutil.DB(t)
	api := &httpapi.API{Store: db, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Credentials: httpapi.DemoCredentials()}
	return api.Handler()
}
func call(h http.Handler, method, path, key, body, idempotency string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if key != "" {
		r.Header.Set("Authorization", "Bearer "+key)
	}
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	if idempotency != "" {
		r.Header.Set("Idempotency-Key", idempotency)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

const valid = `{"reference":"HTTP-DEMO","ttl_seconds":300,"items":[{"sku":"CTRL-101","quantity":2}]}`

func TestAuthorization(t *testing.T) {
	h := handler(t)
	for _, key := range []string{"", "bad-key"} {
		w := call(h, "GET", "/api/v1/stock", key, "", "")
		if w.Code != 401 || !strings.Contains(w.Body.String(), "request_id") {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if w := call(h, "GET", "/api/v1/stock", "demo-viewer-a", "", ""); w.Code != 200 {
		t.Fatal(w.Code)
	}
}
func TestRoleEnforcement(t *testing.T) {
	h := handler(t)
	for _, path := range []string{"/api/v1/reservations", "/api/v1/reservations/demo0000000000000000000000000001/confirm", "/api/v1/events/1/retry"} {
		w := call(h, "POST", path, "demo-viewer-a", valid, "viewer-key-1")
		if w.Code != 403 {
			t.Fatal(path, w.Code)
		}
	}
	if w := call(h, "GET", "/api/v1/metrics", "demo-viewer-a", "", ""); w.Code != 403 {
		t.Fatal(w.Code)
	}
}
func TestCreateHTTPReplay(t *testing.T) {
	h := handler(t)
	w := call(h, "POST", "/api/v1/reservations", "demo-operator-a", valid, "http-request-1")
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	first := w.Body.String()
	w = call(h, "POST", "/api/v1/reservations", "demo-operator-a", valid, "http-request-1")
	if w.Code != 200 || w.Header().Get("Idempotency-Replayed") != "true" || w.Body.String() != first {
		t.Fatal(w.Code, w.Body.String())
	}
	w = call(h, "POST", "/api/v1/reservations", "demo-operator-a", strings.Replace(valid, `"quantity":2`, `"quantity":3`, 1), "http-request-1")
	if w.Code != 409 {
		t.Fatal(w.Code)
	}
}
func TestStrictJSON(t *testing.T) {
	h := handler(t)
	cases := []string{`null`, `[]`, valid + `{}`, strings.Replace(valid, `"quantity":2`, `"quantity":"2"`, 1), strings.Replace(valid, `"quantity":2`, `"quantity":2.5`, 1), strings.Replace(valid, `"reference":"HTTP-DEMO"`, `"tenant":"factory-b","reference":"HTTP-DEMO"`, 1), `{"reference":"A","ttl_seconds":300,"items":[{"sku":"CTRL-101","quantity":999999999999999999999}]}`, `{"reference":`}
	for _, body := range cases {
		w := call(h, "POST", "/api/v1/reservations", "demo-manager-a", body, "invalid-json-key")
		if w.Code != 400 {
			t.Fatalf("body %s code %d: %s", body, w.Code, w.Body.String())
		}
	}
}
func TestMediaTypeAndSize(t *testing.T) {
	h := handler(t)
	r := httptest.NewRequest("POST", "/api/v1/reservations", strings.NewReader(valid))
	r.Header.Set("Authorization", "Bearer demo-manager-a")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 415 {
		t.Fatal(w.Code)
	}
	body := `{"reference":"` + strings.Repeat("A", 70000) + `","ttl_seconds":300,"items":[]}`
	if w = call(h, "POST", "/api/v1/reservations", "demo-manager-a", body, "too-large-key"); w.Code != 400 {
		t.Fatal(w.Code)
	}
}
func TestMissingIdempotencyKey(t *testing.T) {
	h := handler(t)
	w := call(h, "POST", "/api/v1/reservations", "demo-manager-a", valid, "")
	if w.Code != 400 {
		t.Fatal(w.Code)
	}
}
func TestPaginationValidation(t *testing.T) {
	h := handler(t)
	for _, query := range []string{"limit=0", "limit=101", "limit=1.1", "limit=x", "offset=-1", "offset=100001"} {
		if w := call(h, "GET", "/api/v1/reservations?"+query, "demo-manager-a", "", ""); w.Code != 400 {
			t.Fatal(query, w.Code)
		}
	}
	w := call(h, "GET", "/api/v1/reservations?limit=2&offset=0", "demo-manager-a", "", "")
	var body struct {
		Data  []json.RawMessage `json:"data"`
		Total int               `json:"total"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &body) != nil || len(body.Data) != 2 || body.Total != 3 {
		t.Fatal(w.Code, w.Body.String())
	}
}
func TestTenantReadIsolationHTTP(t *testing.T) {
	h := handler(t)
	w := call(h, "POST", "/api/v1/reservations", "demo-manager-a", valid, "tenant-http-key")
	var r struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &r); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/v1/reservations/" + r.ID, "/api/v1/reservations/" + r.ID + "/audit"} {
		if w = call(h, "GET", path, "demo-manager-b", "", ""); w.Code != 404 {
			t.Fatal(path, w.Code)
		}
	}
	w = call(h, "GET", "/api/v1/stock", "demo-manager-b", "", "")
	if w.Code != 200 || strings.Contains(w.Body.String(), "CTRL-101") {
		t.Fatal(w.Body.String())
	}
	w = call(h, "GET", "/api/v1/events", "demo-manager-b", "", "")
	if w.Body.String() != "{\"data\":[]}" {
		t.Fatal(w.Body.String())
	}
}
func TestTransitionHTTP(t *testing.T) {
	h := handler(t)
	w := call(h, "POST", "/api/v1/reservations", "demo-manager-a", valid, "transition-http-key")
	var r struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &r)
	w = call(h, "POST", "/api/v1/reservations/"+r.ID+"/confirm", "demo-manager-a", `{"version":1}`, "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"confirmed"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	w = call(h, "GET", "/api/v1/reservations/"+r.ID+"/audit", "demo-manager-a", "", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "reservation.confirmed") {
		t.Fatal(w.Code, w.Body.String())
	}
}
func TestHealthStaticAndHeaders(t *testing.T) {
	h := handler(t)
	for _, path := range []string{"/", "/healthz", "/readyz", "/assets/app.js"} {
		w := call(h, "GET", path, "", "", "")
		if w.Code != 200 {
			t.Fatal(path, w.Code)
		}
		if w.Header().Get("X-Content-Type-Options") != "nosniff" || w.Header().Get("X-Request-ID") == "" || w.Header().Get("Content-Security-Policy") == "" {
			t.Fatal("missing security headers")
		}
	}
	if w := call(h, "GET", "/api/v1/session", "demo-manager-a", "", ""); w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("private response cacheable")
	}
}
func TestMetricsAndNoTokenLogs(t *testing.T) {
	db := testutil.DB(t)
	var buf bytes.Buffer
	api := &httpapi.API{Store: db, Logger: slog.New(slog.NewJSONHandler(&buf, nil)), Credentials: httpapi.DemoCredentials()}
	h := api.Handler()
	w := call(h, "GET", "/api/v1/metrics?token=secret-ref", "demo-manager-a", "", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "stockflow_http_requests_total") {
		t.Fatal(w.Code)
	}
	if strings.Contains(buf.String(), "demo-manager-a") || strings.Contains(buf.String(), "secret-ref") {
		t.Fatal("logged credential/query")
	}
}
func TestUnknownRouteAndRetryID(t *testing.T) {
	h := handler(t)
	if w := call(h, "GET", "/api/v1/unknown", "demo-manager-a", "", ""); w.Code != 404 {
		t.Fatal(w.Code)
	}
	if w := call(h, "POST", "/api/v1/events/not-a-number/retry", "demo-manager-a", "", ""); w.Code != 400 {
		t.Fatal(w.Code)
	}
	if w := call(h, "POST", "/api/v1/events/9223372036854775807/retry", "demo-manager-a", "", ""); w.Code != 404 {
		t.Fatal(w.Code)
	}
}

func TestRejectJSONLikeContentType(t *testing.T) {
	h := handler(t)
	for _, media := range []string{"application/jsonp", "application/json-invalid", "application/json;broken"} {
		r := httptest.NewRequest("POST", "/api/v1/reservations", strings.NewReader(valid))
		r.Header.Set("Authorization", "Bearer demo-manager-a")
		r.Header.Set("Idempotency-Key", "media-type-test")
		r.Header.Set("Content-Type", media)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 415 {
			t.Fatalf("accepted unsupported media type %s: %d", media, w.Code)
		}
	}
	r := httptest.NewRequest("POST", "/api/v1/reservations", strings.NewReader(valid))
	r.Header.Set("Authorization", "Bearer demo-manager-a")
	r.Header.Set("Idempotency-Key", "media-type-valid")
	r.Header.Set("Content-Type", "application/json; charset=utf-8")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 201 {
		t.Fatalf("valid charset rejected: %d", w.Code)
	}
}
