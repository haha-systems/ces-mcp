package httptransport

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"net"
	"net/http"
	pathpkg "path"
	"strings"
	"time"
)

type Config struct {
	Address     string
	Path        string
	BearerToken string
	TLSCertFile string
	TLSKeyFile  string
}

func New(handler http.Handler, config Config) (*http.Server, error) {
	if handler == nil {
		return nil, errors.New("MCP HTTP handler is required")
	}

	if config.Address == "" {
		config.Address = "127.0.0.1:4096"
	}

	if config.Path == "" {
		config.Path = "/mcp"
	}

	if err := validateAddress(config.Address); err != nil {
		return nil, err
	}

	if !strings.HasPrefix(config.Path, "/") || strings.ContainsAny(config.Path, "?#{}\\") ||
		pathpkg.Clean(config.Path) != config.Path {
		return nil, errors.New(
			"MCP HTTP path must be a clean absolute path without a query, fragment, or pattern syntax",
		)
	}

	if strings.ContainsAny(config.BearerToken, " \t\r\n") {
		return nil, errors.New("CES_HTTP_BEARER_TOKEN must not contain whitespace")
	}

	if (config.TLSCertFile == "") != (config.TLSKeyFile == "") {
		return nil, errors.New(
			"both CES_HTTP_TLS_CERT_FILE and CES_HTTP_TLS_KEY_FILE are required for TLS",
		)
	}

	if !isLoopbackAddress(config.Address) && strings.TrimSpace(config.BearerToken) == "" {
		return nil, errors.New(
			"CES_HTTP_BEARER_TOKEN is required when binding to a non-loopback address",
		)
	}

	mux := http.NewServeMux()
	mux.Handle(config.Path, handler)

	var root http.Handler = mux
	if config.BearerToken != "" {
		root = bearerAuth(config.BearerToken, root)
	}

	root = http.NewCrossOriginProtection().Handler(root)

	return &http.Server{
		Addr:              config.Address,
		Handler:           root,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    1 << 20,
	}, nil
}

func validateAddress(address string) error {
	if _, _, err := net.SplitHostPort(address); err != nil {
		return fmt.Errorf("CES_HTTP_ADDR must contain a host and port: %w", err)
	}

	return nil
}

func isLoopbackAddress(address string) bool {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return false
	}

	if strings.EqualFold(strings.TrimSuffix(host, "."), "localhost") {
		return true
	}

	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}

	return false
}

func bearerAuth(expected string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fields := strings.Fields(r.Header.Get("Authorization"))
		if len(fields) != 2 || !strings.EqualFold(fields[0], "Bearer") ||
			subtle.ConstantTimeCompare([]byte(fields[1]), []byte(expected)) != 1 {
			w.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(w, "unauthorized", http.StatusUnauthorized)

			return
		}

		next.ServeHTTP(w, r)
	})
}
