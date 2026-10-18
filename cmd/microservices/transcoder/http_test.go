package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rtcdance/streamgate/pkg/monitoring"
	"github.com/stretchr/testify/require"
)

func TestWorkerMetrics_ProtectedAndScrapable(t *testing.T) {
	const token = "worker-metrics-test-token"
	handler := newWorkerHTTPHandler(token)
	monitoring.TranscodingDurationSeconds.WithLabelValues("360p").Observe(0.5)
	for _, test := range []struct {
		name   string
		values []string
		status int
	}{
		{"missing", nil, http.StatusUnauthorized},
		{"wrong", []string{"Bearer wrong"}, http.StatusUnauthorized},
		{"raw", []string{token}, http.StatusUnauthorized},
		{"duplicate", []string{"Bearer " + token, "Bearer " + token}, http.StatusUnauthorized},
		{"conflict", []string{"Bearer " + token, "Bearer wrong"}, http.StatusUnauthorized},
		{"valid", []string{"Bearer " + token}, http.StatusOK},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/metrics", http.NoBody)
			for _, value := range test.values {
				request.Header.Add("Authorization", value)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			require.Equal(t, test.status, response.Code)
			require.NotContains(t, response.Body.String(), token)
			if test.status == http.StatusOK {
				require.Contains(t, response.Body.String(), "streamgate_transcoding_duration_seconds")
				require.Contains(t, response.Body.String(), "process_resident_memory_bytes")
				require.Contains(t, response.Body.String(), "process_cpu_seconds_total")
			}
		})
	}
	for _, path := range []string{"/health", "/api/v1/transcode"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, http.NoBody))
		if path == "/health" {
			require.Equal(t, http.StatusOK, response.Code)
		} else {
			require.Equal(t, http.StatusNotFound, response.Code)
		}
	}
	response := httptest.NewRecorder()
	newWorkerHTTPHandler("").ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", http.NoBody))
	require.Equal(t, http.StatusOK, response.Code, "unset token preserves the existing internal scrape policy")
}
