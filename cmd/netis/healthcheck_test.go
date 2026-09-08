package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHealthAddr(t *testing.T) {
	cases := map[string]string{
		":8080":          "127.0.0.1:8080",
		"0.0.0.0:8080":   "127.0.0.1:8080",
		"[::]:8080":      "127.0.0.1:8080",
		"127.0.0.1:9999": "127.0.0.1:9999",
		"192.168.1.5:80": "192.168.1.5:80",
		"[::1]:8080":     "[::1]:8080",
		"not-an-address": "not-an-address",
	}
	for in, want := range cases {
		if got := healthAddr(in); got != want {
			t.Errorf("healthAddr(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRunHealthcheck(t *testing.T) {
	var status int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			t.Errorf("probed %q, want /healthz", r.URL.Path)
		}
		w.WriteHeader(status)
	}))
	defer srv.Close()
	addr := strings.TrimPrefix(srv.URL, "http://")

	status = 200
	t.Setenv("NETIS_ADDR", addr)
	if err := runHealthcheck(context.Background()); err != nil {
		t.Errorf("healthy server: %v", err)
	}

	status = 503
	if err := runHealthcheck(context.Background()); err == nil {
		t.Error("a 503 must fail the healthcheck")
	}
}

// Nothing listening is the case a container HEALTHCHECK exists to catch.
func TestRunHealthcheckNoServer(t *testing.T) {
	t.Setenv("NETIS_ADDR", "127.0.0.1:1") // reserved, nothing listens here
	if err := runHealthcheck(context.Background()); err == nil {
		t.Error("healthcheck against a dead port must fail")
	}
}
