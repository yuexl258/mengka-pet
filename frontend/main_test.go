package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"testing/fstest"
)

func TestHandlerServesStaticFilesAndSPAFallback(t *testing.T) {
	dist := fstest.MapFS{
		"index.html":    {Data: []byte("index")},
		"assets/app.js": {Data: []byte("app")},
	}
	backendURL, _ := url.Parse("http://127.0.0.1:1")
	handler := newHandler(dist, backendURL)

	for requestPath, want := range map[string]string{
		"/assets/app.js": "app",
		"/user/profile":  "index",
	} {
		request := httptest.NewRequest(http.MethodGet, requestPath, nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK || response.Body.String() != want {
			t.Fatalf("GET %s = (%d, %q), want (200, %q)", requestPath, response.Code, response.Body.String(), want)
		}
	}
}

func TestHandlerProxiesAPIRequests(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, r.Host+" "+r.URL.RequestURI())
	}))
	defer backend.Close()
	backendURL, _ := url.Parse(backend.URL)
	handler := newHandler(fstest.MapFS{"index.html": {Data: []byte("index")}}, backendURL)

	request := httptest.NewRequest(http.MethodGet, "/api/version?source=test", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	want := backendURL.Host + " /api/version?source=test"
	if response.Code != http.StatusOK || response.Body.String() != want {
		t.Fatalf("proxy response = (%d, %q)", response.Code, response.Body.String())
	}
}
