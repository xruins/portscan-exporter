package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	// CLI flags
	listenAddr   = flag.String("listen-addr", ":8080", "Address to listen on for metrics")
	targets      = flag.String("targets", "localhost", "Comma-separated list of hosts to scan")
	portRange    = flag.String("port-range", "1-1024", "Range of ports to scan (format: start-end)")
	maxWorkers   = flag.Int("max-workers", 100, "Maximum number of concurrent port scan workers")
	scanTimeout  = flag.Duration("timeout", 2*time.Second, "Timeout for port connection attempts")
	scanInterval = flag.Duration("scan-interval", 3*time.Hour, "Interval between scans")
	initialDelay = flag.Duration("initial-delay", 5*time.Minute, "Delay before first scan starts")
	logFormat    = flag.String("log-format", "text", "Log format (text or json)")
	logLevel     = flag.String("log-level", "info", "Log level (debug, info, warn, error)")

	// Prometheus metrics
	openPortsCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "portscan_open_ports_count",
			Help: "Number of open ports",
		},
		[]string{"target"},
	)
	closedPortsCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "portscan_closed_ports_count",
			Help: "Number of closed ports",
		},
		[]string{"target"},
	)
	filteredPortsCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "portscan_filtered_ports_count",
			Help: "Number of filtered ports",
		},
		[]string{"target"},
	)
	openPortStatus = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "portscan_port_status",
			Help: "Status of individual ports (1 = open)",
		},
		[]string{"target", "port"},
	)
	// New histogram metric for port scan duration
	portScanDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "portscan_port_duration_seconds",
			Help:    "Time taken to scan each port in seconds",
			Buckets: prometheus.ExponentialBuckets(0.001, 2, 10), // From 1ms to ~1s
		},
		[]string{"target", "port", "status"},
	)
	// New metric for the last completed scan timestamp
	lastScanTimestamp = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "portscan_last_scan_timestamp",
			Help: "Unix timestamp of the last completed scan",
		},
		[]string{"target"},
	)
	// New metric for scan status
	scanStatus = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "portscan_scan_status",
			Help: "Status of the last scan (1 = success, 0 = failed)",
		},
		[]string{"target", "status"},
	)
	// Counter for total completed scans
	scanCompletedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "portscan_completed_total",
			Help: "Total number of completed scan operations (successful or failed)",
		},
		[]string{"target"},
	)
)

func init() {
	// Register metrics with Prometheus
	prometheus.MustRegister(openPortsCount)
	prometheus.MustRegister(closedPortsCount)
	prometheus.MustRegister(filteredPortsCount)
	prometheus.MustRegister(openPortStatus)
	prometheus.MustRegister(portScanDuration)
	prometheus.MustRegister(lastScanTimestamp)
	prometheus.MustRegister(scanStatus)
	prometheus.MustRegister(scanCompletedTotal)
}

type PortStatus int

const (
	PortOpen PortStatus = iota
	PortClosed
	PortFiltered
)

func (s PortStatus) String() string {
	switch s {
	case PortOpen:
		return "open"
	case PortClosed:
		return "closed"
	case PortFiltered:
		return "filtered"
	default:
		return "unknown"
	}
}

func setupLogger(format, level string) {
	var handler slog.Handler

	// Set log level
	var logLevelValue slog.Level
	switch level {
	case "debug":
		logLevelValue = slog.LevelDebug
	case "info":
		logLevelValue = slog.LevelInfo
	case "warn":
		logLevelValue = slog.LevelWarn
	case "error":
		logLevelValue = slog.LevelError
	default:
		logLevelValue = slog.LevelInfo
	}

	// Set log format
	switch format {
	case "json":
		handler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			Level: logLevelValue,
		})
	default: // "text" or any other value
		handler = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
			Level: logLevelValue,
		})
	}

	logger := slog.New(handler)
	slog.SetDefault(logger)
}

func main() {
	flag.Parse()

	// Set up logger based on command-line arguments
	setupLogger(*logFormat, *logLevel)

	// Parse targets using strings.Split directly
	targetList := strings.Split(*targets, ",")
	if len(targetList) == 0 {
		slog.Error("No targets specified")
		os.Exit(1)
	}

	// Parse port range
	startPort, endPort, err := parsePortRange(*portRange)
	if err != nil {
		slog.Error("Invalid port range", "error", err)
		os.Exit(1)
	}

	// Set up HTTP server for metrics
	http.Handle("/metrics", promhttp.Handler())
	go func() {
		slog.Info("Starting metrics server", "address", *listenAddr)
		if err := http.ListenAndServe(*listenAddr, nil); err != nil {
			slog.Error("Error starting metrics server", "error", err)
			os.Exit(1)
		}
	}()

	// Log scan interval and initial delay
	slog.Info("Port scanner configured",
		"scan_interval", *scanInterval,
		"initial_delay", *initialDelay)

	// Wait for the initial delay before starting scans
	slog.Info("Waiting for initial delay before starting first scan", "delay", *initialDelay)
	time.Sleep(*initialDelay)
	slog.Info("Initial delay completed, starting scans")

	// Run port scanner in a loop
	for {
		for _, target := range targetList {
			go scanTarget(target, startPort, endPort, *maxWorkers, *scanTimeout)
		}
		time.Sleep(*scanInterval) // Use configurable scan interval
	}
}

func scanTarget(target string, startPort, endPort, maxConcurrent int, timeout time.Duration) {
	ctx := context.Background()
	startTime := time.Now()

	slog.LogAttrs(ctx, slog.LevelInfo, "Starting scan",
		slog.String("target", target),
		slog.Int("start_port", startPort),
		slog.Int("end_port", endPort))

	// Set up atomic counters
	var openCount, closedCount, filteredCount atomic.Int64

	// Use a semaphore to limit concurrency
	sem := make(chan struct{}, maxConcurrent)
	var wg sync.WaitGroup

	// Scan ports
	for port := startPort; port <= endPort; port++ {
		wg.Add(1)
		sem <- struct{}{} // Acquire semaphore
		go func(p int) {
			defer wg.Done()
			defer func() { <-sem }() // Release semaphore

			status, duration := scanPort(target, p, timeout)

			// Record duration in the histogram
			portLabel := fmt.Sprintf("%d", p)
			portScanDuration.WithLabelValues(target, portLabel, status.String()).Observe(duration.Seconds())

			// Update counters using atomic operations
			switch status {
			case PortOpen:
				openCount.Add(1)
				openPortStatus.WithLabelValues(target, portLabel).Set(1)
				slog.Debug("Port is open", "target", target, "port", p, "duration", duration)
			case PortClosed:
				closedCount.Add(1)
				openPortStatus.WithLabelValues(target, portLabel).Set(0)
			case PortFiltered:
				filteredCount.Add(1)
				// No need to set individual filtered ports
			}
		}(port)
	}

	wg.Wait() // Wait for all scans to complete

	// Calculate total scan duration
	totalDuration := time.Since(startTime)
	totalDurationMs := totalDuration.Milliseconds()

	// Update metrics using values from atomic counters
	openPortsCount.WithLabelValues(target).Set(float64(openCount.Load()))
	closedPortsCount.WithLabelValues(target).Set(float64(closedCount.Load()))
	filteredPortsCount.WithLabelValues(target).Set(float64(filteredCount.Load()))

	// Update last scan timestamp (in Unix time - seconds since epoch)
	lastScanTimestamp.WithLabelValues(target).Set(float64(time.Now().Unix()))

	// Increment total completed scans counter
	scanCompletedTotal.WithLabelValues(target).Inc()

	slog.LogAttrs(ctx, slog.LevelInfo, "Scan completed",
		slog.String("target", target),
		slog.Int64("open", openCount.Load()),
		slog.Int64("closed", closedCount.Load()),
		slog.Int64("filtered", filteredCount.Load()),
		slog.Duration("total_duration", totalDuration),
		slog.Int64("total_duration_ms", totalDurationMs))
}

// Modified to return duration along with port status
func scanPort(target string, port int, timeout time.Duration) (PortStatus, time.Duration) {
	startTime := time.Now()
	address := net.JoinHostPort(target, fmt.Sprintf("%d", port))
	conn, err := net.DialTimeout("tcp", address, timeout)
	duration := time.Since(startTime)

	if err != nil {
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			return PortFiltered, duration
		}
		return PortClosed, duration
	}

	conn.Close()
	return PortOpen, duration
}

func parsePortRange(s string) (int, int, error) {
	var start, end int
	_, err := fmt.Sscanf(s, "%d-%d", &start, &end)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid port range format (should be start-end): %v", err)
	}

	if start < 1 || start > 65535 || end < 1 || end > 65535 || start > end {
		return 0, 0, fmt.Errorf("port range must be between 1-65535 and start must be <= end")
	}

	return start, end, nil
}
