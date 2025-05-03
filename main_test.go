package main

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"
)

// TestParsePortRange tests the parsePortRange function
func TestParsePortRange(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantStart int
		wantEnd   int
		wantErr   bool
	}{
		{"valid range", "1-1024", 1, 1024, false},
		{"single port", "80-80", 80, 80, false},
		{"inverted range", "1024-1", 0, 0, true},
		{"negative port", "-1-10", 0, 0, true},
		{"non-numeric", "abc-def", 0, 0, true},
		{"partial format", "100", 0, 0, true},
		{"excess values", "100-200-300", 0, 0, true},
		{"too large", "1-70000", 0, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, end, err := parsePortRange(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parsePortRange() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if start != tt.wantStart {
				t.Errorf("parsePortRange() start = %v, want %v", start, tt.wantStart)
			}
			if end != tt.wantEnd {
				t.Errorf("parsePortRange() end = %v, want %v", end, tt.wantEnd)
			}
		})
	}
}

// TestScanPort tests the scanPort function
func TestScanPort(t *testing.T) {
	// Start a mock server to test scanning
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create test listener: %v", err)
	}
	
	// Start test HTTP server
	server := &http.Server{}
	go server.Serve(listener)
	defer server.Shutdown(context.Background())
	
	// Extract port number from listener
	_, portStr, _ := net.SplitHostPort(listener.Addr().String())
	
	// Execute tests
	tests := []struct {
		name    string
		target  string
		port    string
		timeout time.Duration
		want    PortStatus
	}{
		{"open port", "127.0.0.1", portStr, 1 * time.Second, PortOpen},
		{"closed port", "127.0.0.1", "45678", 1 * time.Second, PortClosed},
		{"filter