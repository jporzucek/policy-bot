// Copyright 2024 Palantir Technologies, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package server

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rs/zerolog"
)

// stubTransport returns a fixed response with an optional body for every request.
type stubTransport struct {
	statusCode int
	body       string
}

func (s stubTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	rec := httptest.NewRecorder()
	rec.Header().Set("X-GitHub-Request-Id", "req-123")
	rec.WriteHeader(s.statusCode)
	if s.body != "" {
		rec.WriteString(s.body)
	}
	return rec.Result(), nil
}

func runLogTest(t *testing.T, statusCode int, wantLevel string) {
	t.Helper()
	var buf bytes.Buffer
	logger := zerolog.New(&buf)

	mw := logClientErrors(logger)
	rt := mw(stubTransport{statusCode: statusCode})

	req := httptest.NewRequest(http.MethodGet, "https://api.github.com/repos/test/repo", nil)
	_, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	logOutput := buf.String()
	if !strings.Contains(logOutput, `"level":"`+wantLevel+`"`) {
		t.Errorf("expected level %q in log output, got: %s", wantLevel, logOutput)
	}
	if !strings.Contains(logOutput, `"status":`+itoa(statusCode)) {
		t.Errorf("expected status %d in log output, got: %s", statusCode, logOutput)
	}
	if !strings.Contains(logOutput, `"github_request_id":"req-123"`) {
		t.Errorf("expected github_request_id in log output, got: %s", logOutput)
	}
	if !strings.Contains(logOutput, `"url":"https://api.github.com/repos/test/repo"`) {
		t.Errorf("expected url in log output, got: %s", logOutput)
	}
	if !strings.Contains(logOutput, `"method":"GET"`) {
		t.Errorf("expected method GET in log output, got: %s", logOutput)
	}
}

func TestLogClientErrors_4xx(t *testing.T) {
	runLogTest(t, 404, "warn")
}

func TestLogClientErrors_403(t *testing.T) {
	runLogTest(t, 403, "warn")
}

func TestLogClientErrors_422(t *testing.T) {
	runLogTest(t, 422, "warn")
}

func TestLogClientErrors_5xx(t *testing.T) {
	runLogTest(t, 500, "error")
}

func TestLogClientErrors_503(t *testing.T) {
	runLogTest(t, 503, "error")
}

func TestLogClientErrors_2xxNoLog(t *testing.T) {
	var buf bytes.Buffer
	logger := zerolog.New(&buf)

	mw := logClientErrors(logger)
	rt := mw(stubTransport{statusCode: 200})

	req := httptest.NewRequest(http.MethodGet, "https://api.github.com/repos/test/repo", nil)
	_, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if buf.Len() != 0 {
		t.Errorf("expected no log output for 2xx, got: %s", buf.String())
	}
}

func TestLogClientErrors_ResponseBodyCaptured(t *testing.T) {
	var buf bytes.Buffer
	logger := zerolog.New(&buf)

	errorBody := `{"message":"Not Found","documentation_url":"https://docs.github.com/rest"}`
	mw := logClientErrors(logger)
	rt := mw(stubTransport{statusCode: 404, body: errorBody})

	req := httptest.NewRequest(http.MethodGet, "https://api.github.com/repos/test/repo", nil)
	res, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	logOutput := buf.String()
	// The response body is JSON-escaped in the zerolog output, so check for
	// the key content rather than the exact raw string.
	if !strings.Contains(logOutput, "Not Found") {
		t.Errorf("expected response body content 'Not Found' in log output, got: %s", logOutput)
	}
	if !strings.Contains(logOutput, "response_body") {
		t.Errorf("expected response_body field in log output, got: %s", logOutput)
	}

	// Verify the body is still readable downstream after we captured it.
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("unexpected error reading restored body: %v", err)
	}
	if string(body) != errorBody {
		t.Errorf("expected body %q, got %q", errorBody, string(body))
	}
}

func TestLogClientErrors_3xxNoLog(t *testing.T) {
	var buf bytes.Buffer
	logger := zerolog.New(&buf)

	mw := logClientErrors(logger)
	rt := mw(stubTransport{statusCode: 301})

	req := httptest.NewRequest(http.MethodGet, "https://api.github.com/repos/test/repo", nil)
	_, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if buf.Len() != 0 {
		t.Errorf("expected no log output for 3xx, got: %s", buf.String())
	}
}

// itoa avoids importing strconv for a single call.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf []byte
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		buf = append([]byte{byte('0' + n%10)}, buf...)
		n /= 10
	}
	if neg {
		buf = append([]byte{'-'}, buf...)
	}
	return string(buf)
}
