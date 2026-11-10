package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWordServiceReadiness(t *testing.T) {
	app := testApp(t)
	app.serverMode = true
	app.office = "fixture-engine"
	server := httptest.NewServer(app.handler())
	defer server.Close()
	address := strings.TrimPrefix(server.URL, "http://")
	if err := checkWordService(address, app.token); err != nil {
		t.Fatal(err)
	}
	if err := checkWordService(address, "wrong-token"); err == nil {
		t.Fatal("wrong token accepted")
	}
	app.office = ""
	if err := checkWordService(address, app.token); err == nil {
		t.Fatal("missing engine accepted")
	}
	if err := checkWordService("192.0.2.1:8787", app.token); err == nil {
		t.Fatal("external address accepted")
	}
}

func TestUnreadyServicePreventsNginxConfiguration(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	defer server.Close()
	address := strings.TrimPrefix(server.URL, "http://")
	if err := checkWordService(address, "test-token-with-at-least-24-chars"); err == nil {
		t.Fatal("unavailable service accepted")
	}
}
