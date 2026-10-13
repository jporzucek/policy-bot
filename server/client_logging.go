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

	"github.com/palantir/go-githubapp/githubapp"
	"github.com/rs/zerolog"
)

// maxErrorBodyLen limits how much of the response body is captured in logs.
// GitHub error responses are small JSON objects, but cap to be safe.
const maxErrorBodyLen = 4096

// logClientErrors returns a ClientMiddleware that logs GitHub API responses
// with 4xx and 5xx status codes. 4xx responses are logged at warn level and
// 5xx at error level. The response body (which contains GitHub's error
// message explaining why the request failed) is captured and restored so the
// caller still receives it. This complements githubapp.ClientMetrics, which
// only increments counters without logging, making it difficult to identify
// which endpoints and status codes are driving error volume.
func logClientErrors(logger zerolog.Logger) githubapp.ClientMiddleware {
	return func(next http.RoundTripper) http.RoundTripper {
		return roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			res, err := next.RoundTrip(r)
			if err == nil && res != nil && res.StatusCode >= 400 && res.StatusCode < 600 {
				// Read the full response body to capture GitHub's error message,
				// then restore it so downstream consumers still receive it intact.
				// Only the first maxErrorBodyLen bytes are included in the log
				// to avoid excessive log volume.
				var bodyBytes []byte
				if res.Body != nil {
					bodyBytes, _ = io.ReadAll(res.Body)
					res.Body.Close()
					res.Body = io.NopCloser(bytes.NewReader(bodyBytes))
				}

				logBody := bodyBytes
				if len(logBody) > maxErrorBodyLen {
					logBody = logBody[:maxErrorBodyLen]
				}

				logEvent := logger.Warn()
				if res.StatusCode >= 500 {
					logEvent = logger.Error()
				}
				logEvent.
					Str("method", r.Method).
					Str("url", r.URL.String()).
					Int("status", res.StatusCode).
					Str("github_request_id", res.Header.Get("X-GitHub-Request-Id")).
					Bytes("response_body", logBody).
					Msg("GitHub API returned error status")
			}
			return res, err
		})
	}
}

// roundTripperFunc adapts a function to the http.RoundTripper interface.
type roundTripperFunc func(*http.Request) (*http.Response, error)

func (fn roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return fn(r)
}
