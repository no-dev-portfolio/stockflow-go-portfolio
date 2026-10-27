package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHealthURL(t *testing.T) {
	cases := map[string]string{"": "http://127.0.0.1:8080/readyz", ":9090": "http://127.0.0.1:9090/readyz", "0.0.0.0:9090": "http://127.0.0.1:9090/readyz", "127.0.0.1:9091": "http://127.0.0.1:9091/readyz", "[::]:9090": "http://[::1]:9090/readyz", "[::1]:9091": "http://[::1]:9091/readyz"}
	for address, want := range cases {
		t.Run(address, func(t *testing.T) {
			got, err := healthURL(address)
			if err != nil || got != want {
				t.Fatalf("%q %v", got, err)
			}
		})
	}
	for _, address := range []string{"invalid", "localhost:"} {
		if _, err := healthURL(address); err == nil {
			t.Fatalf("accepted %q", address)
		}
	}
}
func TestCheckHealth(t *testing.T) {
	for _, status := range []int{200, 503} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/readyz" {
					t.Errorf("unexpected path %s", r.URL.Path)
				}
				w.WriteHeader(status)
			}))
			defer server.Close()
			err := checkHealth(strings.TrimPrefix(server.URL, "http://"))
			if (err == nil) != (status == 200) {
				t.Fatalf("status %d: %v", status, err)
			}
		})
	}
	if err := checkHealth("invalid"); err == nil {
		t.Fatal("invalid address accepted")
	}
}
