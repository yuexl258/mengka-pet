package server

import (
	"database/sql"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"qq-pet/backend/internal/config"
)

func TestRequestIDIsReturnedAndPreserved(t *testing.T) {
	router := NewRouter(&sql.DB{}, config.Config{}, slog.Default())
	for _, input := range []string{"", "phase-five-request"} {
		req := httptest.NewRequest(http.MethodGet, "/api/version", nil)
		if input != "" {
			req.Header.Set("X-Request-ID", input)
		}
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		value := res.Header().Get("X-Request-ID")
		if value == "" || (input != "" && value != input) {
			t.Fatalf("request id input=%q output=%q", input, value)
		}
	}
}
