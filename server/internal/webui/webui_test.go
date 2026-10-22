package webui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func get(h http.Handler, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestServesTheBuiltPage(t *testing.T) {
	h := handlerFor(fstest.MapFS{
		"index.html":    {Data: []byte("<html>dash</html>")},
		"assets/app.js": {Data: []byte("x")},
	})

	page := get(h, "/")
	if page.Code != 200 || !strings.Contains(page.Body.String(), "dash") {
		t.Fatalf("page: %d %q", page.Code, page.Body.String())
	}
	if got := page.Header().Get("Cache-Control"); got != "no-cache" {
		t.Errorf("page Cache-Control = %q, want no-cache", got)
	}
	if got := get(h, "/assets/app.js").Header().Get("Cache-Control"); !strings.Contains(got, "immutable") {
		t.Errorf("asset Cache-Control = %q, want immutable", got)
	}
	if get(h, "/nope").Code != 404 {
		t.Error("an unknown path should be 404")
	}
}

func TestSaysSoWhenNotBuilt(t *testing.T) {
	rec := get(handlerFor(fstest.MapFS{".gitkeep": {}}), "/")
	if rec.Code != 404 || !strings.Contains(rec.Body.String(), "not built") {
		t.Errorf("got %d %q", rec.Code, rec.Body.String())
	}
}
