package gloqhttp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/skinleak/gloq"
	"github.com/skinleak/gloq/gloqhttp"
)

func jsonLogger(output *bytes.Buffer) *slog.Logger {
	return slog.New(gloq.NewHandler(output,
		gloq.WithFormat(gloq.FormatJSON),
		gloq.WithLevel(slog.LevelDebug),
		gloq.WithContextAttrs(gloqhttp.ContextAttrs),
	))
}

func entries(t *testing.T, output *bytes.Buffer) []map[string]any {
	t.Helper()
	var result []map[string]any
	for _, line := range bytes.Split(bytes.TrimSpace(output.Bytes()), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var entry map[string]any
		if err := json.Unmarshal(line, &entry); err != nil {
			t.Fatalf("line %q: %v", line, err)
		}
		result = append(result, entry)
	}
	return result
}

func serve(handler http.Handler, request *http.Request) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func TestMiddlewareLogsRequest(t *testing.T) {
	var output bytes.Buffer
	logger := jsonLogger(&output)
	handler := gloqhttp.Middleware(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		logger.InfoContext(r.Context(), "inside handler")
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, "hello")
	}))

	response := serve(handler, httptest.NewRequest(http.MethodPost, "/orders?token=secret", nil))

	id := response.Header().Get(gloqhttp.DefaultRequestIDHeader)
	if len(id) != 32 {
		t.Fatalf("generated request ID = %q", id)
	}
	logged := entries(t, &output)
	if len(logged) != 2 {
		t.Fatalf("records = %v, want 2", logged)
	}
	if logged[0]["msg"] != "inside handler" || logged[0]["request_id"] != id {
		t.Fatalf("handler record = %v, want request_id %q", logged[0], id)
	}
	access := logged[1]
	want := map[string]any{
		"msg": "http request", "level": "INFO", "method": "POST", "path": "/orders",
		"status": float64(201), "bytes": float64(5), "request_id": id,
	}
	for key, value := range want {
		if access[key] != value {
			t.Errorf("access[%q] = %v, want %v", key, access[key], value)
		}
	}
	if _, ok := access["duration"]; !ok {
		t.Error("access record has no duration")
	}
	if strings.Contains(output.String(), "secret") {
		t.Error("the query string was logged")
	}
	if strings.Count(output.String(), id) != 2 {
		t.Errorf("request ID is repeated in a record: %s", output.String())
	}
}

func TestMiddlewareLevels(t *testing.T) {
	tests := []struct {
		status int
		level  string
	}{
		{0, "INFO"},
		{http.StatusOK, "INFO"},
		{http.StatusNotFound, "WARN"},
		{http.StatusServiceUnavailable, "ERROR"},
	}
	for _, test := range tests {
		var output bytes.Buffer
		handler := gloqhttp.Middleware(jsonLogger(&output))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if test.status != 0 {
				w.WriteHeader(test.status)
			}
		}))
		serve(handler, httptest.NewRequest(http.MethodGet, "/", nil))
		access := entries(t, &output)[0]
		wantStatus := test.status
		if wantStatus == 0 {
			wantStatus = http.StatusOK
		}
		if access["level"] != test.level || access["status"] != float64(wantStatus) {
			t.Errorf("status %d: level %v, status %v; want %s", test.status, access["level"], access["status"], test.level)
		}
	}
}

func TestMiddlewareRequestIDHeader(t *testing.T) {
	var seen string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = gloqhttp.RequestID(r.Context())
	})

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("X-Request-ID", "abc-123")
	response := serve(gloqhttp.Middleware(slog.New(slog.NewTextHandler(io.Discard, nil)))(next), request)
	if seen != "abc-123" || response.Header().Get("X-Request-ID") != "abc-123" {
		t.Fatalf("request ID = %q, response header %q; want the incoming one", seen, response.Header().Get("X-Request-ID"))
	}

	request.Header.Set("X-Request-ID", "bad id\nwith newline")
	serve(gloqhttp.Middleware(slog.New(slog.NewTextHandler(io.Discard, nil)))(next), request)
	if len(seen) != 32 {
		t.Fatalf("invalid incoming ID was used: %q", seen)
	}

	request = httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("Trace-Id", "custom")
	response = serve(gloqhttp.Middleware(slog.New(slog.NewTextHandler(io.Discard, nil)), gloqhttp.WithRequestIDHeader("Trace-Id"))(next), request)
	if seen != "custom" || response.Header().Get("Trace-Id") != "custom" {
		t.Fatalf("custom header: request ID %q", seen)
	}

	request.Header.Set("X-Request-ID", "ignored")
	response = serve(gloqhttp.Middleware(slog.New(slog.NewTextHandler(io.Discard, nil)), gloqhttp.WithRequestIDHeader(""))(next), request)
	if seen == "ignored" || len(seen) != 32 || response.Header().Get("X-Request-ID") != "" {
		t.Fatalf("disabled header: request ID %q, response header %q", seen, response.Header().Get("X-Request-ID"))
	}
}

func TestMiddlewareSkip(t *testing.T) {
	var output bytes.Buffer
	var seen string
	handler := gloqhttp.Middleware(jsonLogger(&output), gloqhttp.WithSkip(func(r *http.Request) bool {
		return r.URL.Path == "/healthz"
	}))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = gloqhttp.RequestID(r.Context())
	}))
	serve(handler, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if output.Len() != 0 {
		t.Fatalf("skipped request was logged: %s", output.String())
	}
	if seen == "" {
		t.Fatal("skipped request has no request ID")
	}
}

func TestMiddlewarePanic(t *testing.T) {
	var output bytes.Buffer
	handler := gloqhttp.Middleware(jsonLogger(&output))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	}))
	func() {
		defer func() {
			if recovered := recover(); recovered != "boom" {
				t.Fatalf("recovered %v, want the original panic", recovered)
			}
		}()
		serve(handler, httptest.NewRequest(http.MethodGet, "/", nil))
	}()
	access := entries(t, &output)[0]
	if access["level"] != "ERROR" || access["panic"] != "boom" {
		t.Fatalf("panic record = %v", access)
	}

	output.Reset()
	handler = gloqhttp.Middleware(jsonLogger(&output))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic(http.ErrAbortHandler)
	}))
	func() {
		defer func() { _ = recover() }()
		serve(handler, httptest.NewRequest(http.MethodGet, "/", nil))
	}()
	if output.Len() != 0 {
		t.Fatalf("ErrAbortHandler was logged: %s", output.String())
	}
}

func TestMiddlewareKeepsResponseWriterFeatures(t *testing.T) {
	var output bytes.Buffer
	handler := gloqhttp.Middleware(jsonLogger(&output))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := w.(http.Flusher); !ok {
			t.Error("response writer is not an http.Flusher")
		}
		_, _ = io.WriteString(w, "part")
		if err := http.NewResponseController(w).Flush(); err != nil {
			t.Errorf("Flush() error = %v", err)
		}
		_, _ = io.Copy(w, strings.NewReader(" and more"))
	}))
	server := httptest.NewServer(handler)
	defer server.Close()

	response, err := http.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if string(body) != "part and more" {
		t.Fatalf("body = %q", body)
	}
	if access := entries(t, &output)[0]; access["bytes"] != float64(len(body)) {
		t.Fatalf("bytes = %v, want %d", access["bytes"], len(body))
	}
}

func TestContextAttrsWithoutRequest(t *testing.T) {
	if attrs := gloqhttp.ContextAttrs(context.Background()); attrs != nil {
		t.Fatalf("ContextAttrs() = %v, want nil", attrs)
	}
	if id := gloqhttp.RequestID(context.Background()); id != "" {
		t.Fatalf("RequestID() = %q, want empty", id)
	}
}
