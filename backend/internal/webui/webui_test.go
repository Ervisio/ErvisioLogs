package webui

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func serve(root fstest.MapFS, method, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	handler(root).ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

func TestSPAFallbackAndAssets(t *testing.T) {
	root := fstest.MapFS{
		"index.html":  {Data: []byte("<html>app</html>")},
		"assets/a.js": {Data: []byte("js")},
	}
	if rec := serve(root, "GET", "/projects/42"); rec.Code != 200 || rec.Body.String() != "<html>app</html>" {
		t.Fatalf("route fallback: %d %q", rec.Code, rec.Body)
	}
	rec := serve(root, "GET", "/assets/a.js")
	if rec.Code != 200 || rec.Header().Get("Cache-Control") == "" {
		t.Fatalf("asset: %d %v", rec.Code, rec.Header())
	}
	if rec := serve(root, "GET", "/assets/missing.js"); rec.Code != 404 {
		t.Fatalf("missing asset must 404, got %d", rec.Code)
	}
	if rec := serve(root, "POST", "/"); rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST: %d", rec.Code)
	}
}

func TestNoBuildReturns404(t *testing.T) {
	if rec := serve(fstest.MapFS{}, "GET", "/"); rec.Code != 404 {
		t.Fatalf("got %d", rec.Code)
	}
}

func TestPathTraversalCannotEscape(t *testing.T) {
	root := fstest.MapFS{"index.html": {Data: []byte("app")}, "../secret": {Data: []byte("root")}}
	if rec := serve(root, "GET", "/../secret"); rec.Body.String() == "root" {
		t.Fatal("escaped")
	}
}
