package httptransport

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNonLoopbackBindingRequiresBearerToken(t *testing.T) {
	_, err := New(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), Config{
		Address: "0.0.0.0:8080", Path: "/mcp",
	})
	if err == nil {
		t.Fatal("non-loopback endpoint was allowed without bearer authentication")
	}

	if _, err := New(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), Config{
		Address: "127.0.0.1:8080", Path: "/mcp",
	}); err != nil {
		t.Fatalf("loopback endpoint should allow local development without a token: %v", err)
	}
}

func TestBearerAuthProtectsStreamableEndpoint(t *testing.T) {
	server, err := New(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}), Config{
		Address: "0.0.0.0:8080", Path: "/mcp", BearerToken: "test-token",
	})
	if err != nil {
		t.Fatal(err)
	}

	httpServer := httptest.NewServer(server.Handler)
	defer httpServer.Close()

	for _, test := range []struct {
		name       string
		header     string
		wantStatus int
	}{
		{name: "missing token", wantStatus: http.StatusUnauthorized},
		{name: "wrong token", header: "Bearer wrong", wantStatus: http.StatusUnauthorized},
		{name: "valid token", header: "Bearer test-token", wantStatus: http.StatusNoContent},
	} {
		t.Run(test.name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodPost, httpServer.URL+"/mcp", nil)
			if err != nil {
				t.Fatal(err)
			}

			if test.header != "" {
				req.Header.Set("Authorization", test.header)
			}

			response, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()

			if response.StatusCode != test.wantStatus {
				t.Fatalf("HTTP status = %d, want %d", response.StatusCode, test.wantStatus)
			}
		})
	}
}

func TestConfigRejectsInvalidPathAndIncompleteTLS(t *testing.T) {
	handler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	for _, config := range []Config{
		{Address: "127.0.0.1:8080", Path: "mcp"},
		{Address: "127.0.0.1:8080", Path: "/mcp?x=1"},
		{Address: "127.0.0.1:8080", Path: "/mcp/{id}"},
		{Address: "127.0.0.1:8080", Path: "/mcp", TLSCertFile: "cert.pem"},
		{Address: "0.0.0.0:8080", Path: "/mcp", BearerToken: "token with space"},
	} {
		if _, err := New(handler, config); err == nil {
			t.Fatalf("invalid configuration was accepted: %+v", config)
		}
	}
}
