package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strconv"
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
		{"filtered port", "10.255.255.255", "80", 100 * time.Millisecond, PortFiltered}, // Test with unreachable IP and short timeout
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var port string
			if tt.name == "open port" {
				_, port, _ = net.SplitHostPort(listener.Addr().String())
			} else {
				port = "45678" // A port that's usually closed
			}

			p, err := strconv.Atoi(port) // Convert port string to int for testing
			if err != nil {
				t.Fatalf("Failed to convert port string to int: %v", err)
			}
			status, duration := scanPort(tt.target, p, tt.timeout)
			if status != tt.want {
				t.Errorf("scanPort() status = %v, want %v", status, tt.want)
			}
			if duration <= 0 {
				t.Errorf("scanPort() duration should be greater than 0, got %v", duration)
			}
		})
	}
}

// TestSetupLogger tests the setupLogger function
func TestSetupLogger(t *testing.T) {
	// Test different formats and log levels
	tests := []struct {
		name   string
		format string
		level  string
	}{
		{"text format, debug level", "text", "debug"},
		{"json format, info level", "json", "info"},
		{"text format, warn level", "text", "warn"},
		{"json format, error level", "json", "error"},
		{"invalid format, default level", "invalid", "info"},
		{"text format, invalid level", "text", "invalid"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Just verify that logger setup completes without error
			// We expect this function not to throw any errors
			setupLogger(tt.format, tt.level)
		})
	}
}

// TestPortStatusString tests the PortStatus String method
func TestPortStatusString(t *testing.T) {
	tests := []struct {
		status PortStatus
		want   string
	}{
		{PortOpen, "open"},
		{PortClosed, "closed"},
		{PortFiltered, "filtered"},
		{PortStatus(999), "unknown"}, // undefined status
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.status.String(); got != tt.want {
				t.Errorf("PortStatus.String() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestScanTarget is an integration test for the entire scanning functionality
// Note: To properly test this function, we would need more extensive mocking
// and interface abstraction. This is a simplified test.
func TestScanTarget(t *testing.T) {
	// Start a test server
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create test listener: %v", err)
	}

	server := &http.Server{}
	go server.Serve(listener)
	defer server.Shutdown(context.Background())

	// Extract port number from listener
	_, portStr, _ := net.SplitHostPort(listener.Addr().String())
	port := 0
	_, err = fmt.Sscanf(portStr, "%d", &port)
	if err != nil {
		t.Fatalf("Failed to parse port: %v", err)
	}

	// Scan only a single port on localhost
	target := "127.0.0.1"
	maxWorkers := 1
	timeout := 500 * time.Millisecond

	// Just check that scanTarget can be called and updates Prometheus metrics
	// Actual validation of return values or metrics is omitted here as it would be complex
	scanTarget(target, port, port, maxWorkers, timeout)

	// Short delay to ensure test success
	// In a real test, this should be replaced with a better synchronization mechanism
	time.Sleep(1 * time.Second)
}
