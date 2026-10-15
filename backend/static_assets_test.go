package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRetainedReleaseAssets(t *testing.T) {
	a, _ := setup(t)
	data := t.TempDir()
	a.config.UploadDir = filepath.Join(data, "uploads")
	retained := filepath.Join(data, "web-assets")
	for _, dir := range []string{retained, filepath.Join(a.config.WebDir, "assets")} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		filepath.Join(retained, "DiscussionView-AbCd1234.js"):           "export const previous = true;",
		filepath.Join(retained, "MailView-EfGh5678.css"):                ".previous { color: blue; }",
		filepath.Join(retained, "index-AbCd1234.html"):                  "must not serve retained html",
		filepath.Join(retained, "plain.js"):                             "must not serve unhashed files",
		filepath.Join(retained, "current-AbCd1234.js"):                  "stale same name",
		filepath.Join(a.config.WebDir, "assets", "current-AbCd1234.js"): "current release wins",
		filepath.Join(a.config.WebDir, "index.html"):                    "<html>current application</html>",
		filepath.Join(data, "private-AbCd1234.js"):                      "private secret",
	}
	for filename, body := range files {
		if err := os.WriteFile(filename, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(data, "private-AbCd1234.js"), filepath.Join(retained, "escape-AbCd1234.js")); err != nil {
		t.Fatal(err)
	}
	handler := a.Handler()
	cases := []struct {
		method, path      string
		status            int
		body, contentType string
	}{
		{"GET", "/assets/DiscussionView-AbCd1234.js", 200, "export const previous = true;", "javascript"},
		{"HEAD", "/assets/DiscussionView-AbCd1234.js", 200, "", "javascript"},
		{"GET", "/assets/MailView-EfGh5678.css", 200, ".previous { color: blue; }", "text/css"},
		{"GET", "/assets/current-AbCd1234.js", 200, "current release wins", "javascript"},
		{"GET", "/assets/missing-AbCd1234.js", 404, "404 page not found\n", "text/plain"},
		{"GET", "/assets/escape-AbCd1234.js", 404, "404 page not found\n", "text/plain"},
		{"GET", "/assets/index-AbCd1234.html", 404, "404 page not found\n", "text/plain"},
		{"GET", "/assets/plain.js", 404, "404 page not found\n", "text/plain"},
		{"GET", "/private-AbCd1234.js", 404, "404 page not found\n", "text/plain"},
		{"GET", "/assets/nested/DiscussionView-AbCd1234.js", 404, "404 page not found\n", "text/plain"},
		{"GET", "/assets/%2e%2e/private-AbCd1234.js", 404, "404 page not found\n", "text/plain"},
		{"GET", "/applications", 200, "<html>current application</html>", "text/html"},
	}
	for _, tc := range cases {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
			if w.Code != tc.status || w.Body.String() != tc.body {
				t.Fatalf("status=%d body=%q", w.Code, w.Body.String())
			}
			if !strings.Contains(w.Header().Get("Content-Type"), tc.contentType) {
				t.Fatalf("content type=%q", w.Header().Get("Content-Type"))
			}
			if tc.status == 200 && strings.HasPrefix(tc.path, "/assets/") && !strings.Contains(w.Header().Get("Cache-Control"), "immutable") {
				t.Fatal("hashed assets must be immutable")
			}
			if tc.path == "/applications" && w.Header().Get("Cache-Control") != "no-cache" {
				t.Fatal("application shell must revalidate")
			}
		})
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/assets/DiscussionView-AbCd1234.js", nil)
	r.Header.Set("Range", "bytes=0-5")
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusPartialContent || w.Body.String() != "export" {
		t.Fatal("retained assets should support range requests")
	}
}
