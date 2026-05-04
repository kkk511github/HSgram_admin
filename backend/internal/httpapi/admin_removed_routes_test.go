package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRemovedReactionAdminRoutesReturnJSONNotFound(t *testing.T) {
	handler := New(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, UpdateConfig{
		ReleasesDir: t.TempDir(),
	})

	cases := []struct {
		method string
		path   string
		body   string
	}{
		{method: http.MethodGet, path: "/api/admin/reactions"},
		{method: http.MethodPost, path: "/api/admin/reactions", body: `{}`},
		{method: http.MethodPatch, path: "/api/admin/reactions/1", body: `{}`},
		{method: http.MethodGet, path: "/api/admin/emoji-keywords"},
		{method: http.MethodPost, path: "/api/admin/emoji-keywords", body: `{}`},
	}
	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404; body=%s", rec.Code, rec.Body.String())
			}
			if got := rec.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
				t.Fatalf("content-type = %q, want json", got)
			}
			if !strings.Contains(rec.Body.String(), `"ok":false`) {
				t.Fatalf("expected api error json, got %s", rec.Body.String())
			}
		})
	}
}
