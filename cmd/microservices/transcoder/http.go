package main

import (
	"crypto/subtle"
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func newWorkerHTTPHandler(metricsToken string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"healthy","service":"transcoder"}`))
	})
	metrics := promhttp.Handler()
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		if metricsToken != "" {
			values := r.Header.Values("Authorization")
			// The ops token is distinct from JWT/chain keys. Never accept a
			// raw token or select one value from conflicting credentials.
			if len(values) != 1 || subtle.ConstantTimeCompare([]byte(values[0]), []byte("Bearer "+metricsToken)) != 1 {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":"valid metrics bearer token required","code":"UNAUTHORIZED"}`))
				return
			}
		}
		metrics.ServeHTTP(w, r)
	})
	return mux
}
