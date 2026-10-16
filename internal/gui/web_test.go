package gui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWebRequiresRunKey(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusTeapot) })
	srv, err := ListenWeb("127.0.0.1:0", ok)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	h := srv.srv.Handler
	host := srv.ln.Addr().String()
	serve := func(method, target string, mod func(*http.Request)) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, target, nil)
		r.Host = host
		if mod != nil {
			mod(r)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		return rec
	}
	withCookie := func(v string) func(*http.Request) {
		return func(r *http.Request) { r.AddCookie(&http.Cookie{Name: keyCookie, Value: v}) }
	}

	if rec := serve("GET", "/api/state", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("no key: status %d", rec.Code)
	}
	if rec := serve("GET", "/?key=wrong", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong key: status %d", rec.Code)
	}
	if rec := serve("GET", "/api/state", withCookie("wrong")); rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong cookie: status %d", rec.Code)
	}

	rec := serve("GET", "/?key="+srv.key, nil)
	if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Set-Cookie"), srv.key) {
		t.Fatalf("link sign-in: status %d, cookie %q", rec.Code, rec.Header().Get("Set-Cookie"))
	}
	if rec := serve("GET", "/api/state", withCookie(srv.key)); rec.Code != http.StatusTeapot {
		t.Fatalf("signed in: status %d", rec.Code)
	}

	// DNS rebinding: another name resolving to 127.0.0.1.
	rec = serve("GET", "/api/state", func(r *http.Request) {
		withCookie(srv.key)(r)
		r.Host = "evil.example:" + strings.Split(host, ":")[1]
	})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("foreign host: status %d", rec.Code)
	}

	post := func(origin string) int {
		return serve("POST", "/api/settings", func(r *http.Request) {
			withCookie(srv.key)(r)
			if origin != "" {
				r.Header.Set("Origin", origin)
			}
		}).Code
	}
	if code := post(""); code != http.StatusForbidden {
		t.Fatalf("post without origin: status %d", code)
	}
	if code := post("http://evil.example"); code != http.StatusForbidden {
		t.Fatalf("cross-origin post: status %d", code)
	}
	if code := post("http://" + host); code != http.StatusTeapot {
		t.Fatalf("same-origin post: status %d", code)
	}
}
