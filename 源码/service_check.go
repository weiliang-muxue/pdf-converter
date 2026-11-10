package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

// The deployment script uses the bundled program to check localhost, so it
// does not need curl, Python or a network package installation on the server.
func checkWordService(address, token string) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("invalid local service address: %w", err)
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return errors.New("service readiness checks require a loopback IP")
	}
	request, err := http.NewRequest(http.MethodGet, "http://"+address+"/api/status", nil)
	if err != nil {
		return err
	}
	request.Header.Set("X-PDF-Token", token)
	transport := &http.Transport{Proxy: nil}
	defer transport.CloseIdleConnections()
	client := &http.Client{
		Timeout:   2 * time.Second,
		Transport: transport,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("Word service is not ready: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("Word service status returned HTTP %d", response.StatusCode)
	}
	var status struct {
		Version   string `json:"version"`
		WordReady bool   `json:"wordReady"`
		Server    bool   `json:"server"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 65536)).Decode(&status); err != nil {
		return errors.New("Word service returned invalid status data")
	}
	if status.Version != version || !status.Server || !status.WordReady {
		return fmt.Errorf("Word service must be version %s with its conversion engine ready", version)
	}
	return nil
}
