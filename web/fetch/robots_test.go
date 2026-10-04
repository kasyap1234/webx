package fetch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func robotsServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			w.Write([]byte(body))
			return
		}
		w.Write([]byte("ok"))
	}))
}

func TestRobotsAllowed(t *testing.T) {
	srv := robotsServer(t, `User-agent: *
Disallow: /admin
Disallow: /private/*
Allow: /private/public
User-agent: webx
Disallow: /nothing
`)
	defer srv.Close()
	rc := NewRobotsChecker()
	must := func(p string) *url.URL {
		t.Helper()
		u, err := url.Parse(srv.URL + p)
		if err != nil {
			t.Fatal(err)
		}
		return u
	}
	// our UA group wins — /nothing disallowed for webx, everything else open
	if rc.Allowed(context.Background(), must("/nothing")) {
		t.Fatal("/nothing should be disallowed for webx UA")
	}
	if !rc.Allowed(context.Background(), must("/admin/x")) {
		t.Fatal("webx UA group should not inherit *-group rules")
	}
	if !rc.Allowed(context.Background(), must("/docs")) {
		t.Fatal("/docs should be allowed")
	}
}

func TestRobotsStarGroup(t *testing.T) {
	srv := robotsServer(t, "User-agent: *\nDisallow: /admin\n")
	defer srv.Close()
	rc := NewRobotsChecker()
	u, _ := url.Parse(srv.URL + "/admin/panel")
	if rc.Allowed(context.Background(), u) {
		t.Fatal("/admin should be disallowed under *")
	}
	u2, _ := url.Parse(srv.URL + "/open")
	if !rc.Allowed(context.Background(), u2) {
		t.Fatal("/open should be allowed")
	}
}

func TestRobotsMissing(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	rc := NewRobotsChecker()
	u, _ := url.Parse(srv.URL + "/anything")
	if !rc.Allowed(context.Background(), u) {
		t.Fatal("missing robots.txt must allow everything")
	}
}
