package registry

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"driftline/go/internal/models"
)

func newTestRouter(t *testing.T, maxUpload int64) http.Handler {
	t.Helper()
	svc, _, _ := newTestService(t)
	r := chi.NewRouter()
	NewHandler(svc, maxUpload).Routes(r)
	return r
}

func multipartBody(t *testing.T, fields map[string]string, files map[string]string) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for k, v := range fields {
		_ = w.WriteField(k, v)
	}
	for field, content := range files {
		fw, err := w.CreateFormFile(field, field)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.WriteString(fw, content)
	}
	_ = w.Close()
	return &buf, w.FormDataContentType()
}

func do(h http.Handler, method, path, contentType string, body io.Reader) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, body)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestHandlerRegisterPromoteDownloadFlow(t *testing.T) {
	h := newTestRouter(t, 1<<20)

	body, ct := multipartBody(t,
		map[string]string{"version": "v1"},
		map[string]string{"model": "onnx-bytes", "feature_schema": validSchema, "metrics": `{"f1":0.34}`},
	)
	rec := do(h, http.MethodPost, "/models/loan_default_xgb/versions", ct, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("register: %d %s", rec.Code, rec.Body)
	}
	var v models.ModelVersion
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	if v.Version != "v1" || string(v.Metrics) != `{"f1":0.34}` {
		t.Fatalf("unexpected body: %s", rec.Body)
	}

	rec = do(h, http.MethodPost, "/models/loan_default_xgb/versions/v1/promote", "application/json",
		strings.NewReader(`{"stage":"production"}`))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"stage":"production"`) {
		t.Fatalf("promote: %d %s", rec.Code, rec.Body)
	}

	rec = do(h, http.MethodGet, "/models/loan_default_xgb/versions/v1/artifact", "", nil)
	if rec.Code != http.StatusOK || rec.Body.String() != "onnx-bytes" || rec.Header().Get("X-Checksum-SHA256") == "" {
		t.Fatalf("download: %d %q", rec.Code, rec.Body)
	}
}

func TestHandlerStatusCodes(t *testing.T) {
	jsonBody := func(s string) func(*testing.T) (io.Reader, string) {
		return func(*testing.T) (io.Reader, string) { return strings.NewReader(s), "application/json" }
	}
	upload := func(fields, files map[string]string) func(*testing.T) (io.Reader, string) {
		return func(t *testing.T) (io.Reader, string) { return multipartBody(t, fields, files) }
	}

	tests := []struct {
		name     string
		method   string
		path     string
		body     func(*testing.T) (io.Reader, string)
		wantCode int
	}{
		{"create model", "POST", "/models", jsonBody(`{"name":"churn"}`), 201},
		{"duplicate model", "POST", "/models", jsonBody(`{"name":"loan_default_xgb"}`), 409},
		{"unknown json field", "POST", "/models", jsonBody(`{"nam":"x"}`), 400},
		{"invalid model name", "POST", "/models", jsonBody(`{"name":"../x"}`), 400},
		{"get missing model", "GET", "/models/ghost", nil, 404},
		{"list versions missing model", "GET", "/models/ghost/versions", nil, 404},
		{"missing version", "GET", "/models/loan_default_xgb/versions/v9", nil, 404},
		{"promote missing version", "POST", "/models/loan_default_xgb/versions/v9/promote", jsonBody(`{"stage":"staging"}`), 404},
		{"upload without schema", "POST", "/models/loan_default_xgb/versions",
			upload(map[string]string{"version": "v1"}, map[string]string{"model": "x"}), 400},
		{"upload without model", "POST", "/models/loan_default_xgb/versions",
			upload(map[string]string{"version": "v1"}, map[string]string{"feature_schema": validSchema}), 400},
		{"upload not multipart", "POST", "/models/loan_default_xgb/versions", jsonBody(`{}`), 400},
		{"upload too large", "POST", "/models/loan_default_xgb/versions",
			upload(map[string]string{"version": "v1"},
				map[string]string{"model": strings.Repeat("x", 4096), "feature_schema": validSchema}), 413},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newTestRouter(t, 2048)
			var body io.Reader
			var ct string
			if tc.body != nil {
				body, ct = tc.body(t)
			}
			rec := do(h, tc.method, tc.path, ct, body)
			if rec.Code != tc.wantCode {
				t.Fatalf("got %d, want %d: %s", rec.Code, tc.wantCode, rec.Body)
			}
		})
	}
}
