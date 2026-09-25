package bma_metrics

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	upstreamsvc "github.com/0xJacky/Nginx-UI/internal/upstream"
	"github.com/gin-gonic/gin"
)

const (
	defaultMetricsPath = "/var/log/nginx/metrics/all.metrics.log"
	maxScannerToken     = 2 * 1024 * 1024
)

type logEntry struct {
	Timestamp        string `json:"timestamp"`
	LB               string `json:"lb"`
	Client           string `json:"client"`
	Host             string `json:"host"`
	ServerPort       string `json:"server_port"`
	Method           string `json:"method"`
	URI              string `json:"uri"`
	Status           string `json:"status"`
	Bytes            string `json:"bytes"`
	Upstream         string `json:"upstream"`
	UpstreamStatus   string `json:"upstream_status"`
	UpstreamConnect  string `json:"upstream_connect"`
	UpstreamResponse string `json:"upstream_response"`
	RequestTime      string `json:"request_time"`
	RequestID        string `json:"request_id"`
}

type Summary struct {
	ActiveClients int     `json:"active_clients"`
	Requests      int64   `json:"requests"`
	RequestsPerS  float64 `json:"requests_per_second"`
	AvgResponseMS float64 `json:"avg_response_ms"`
	AvgConnectMS  float64 `json:"avg_connect_ms"`
	Status4XX     int64   `json:"status_4xx"`
	Status5XX     int64   `json:"status_5xx"`
	Bytes         int64   `json:"bytes"`
}

type ServiceSummary struct {
	Service string  `json:"service"`
	Summary Summary `json:"summary"`
}

type BackendSummary struct {
	Service         string   `json:"service"`
	Backend         string   `json:"backend"`
	BackendName     string   `json:"backend_name"`
	Port            string   `json:"port"`
	Upstreams       []string `json:"upstreams,omitempty"`
	ActiveClients   int      `json:"active_clients"`
	ClientPercent   float64  `json:"client_percent"`
	Requests        int64    `json:"requests"`
	RequestsPerS    float64  `json:"requests_per_second"`
	AvgResponseMS   float64  `json:"avg_response_ms"`
	AvgConnectMS    float64  `json:"avg_connect_ms"`
	Status4XX       int64    `json:"status_4xx"`
	Status5XX       int64    `json:"status_5xx"`
	Bytes           int64    `json:"bytes"`
	Online          *bool    `json:"online,omitempty"`
	HealthLatencyMS float64  `json:"health_latency_ms,omitempty"`
}

type HistoryPoint struct {
	Timestamp      string `json:"timestamp"`
	Service        string `json:"service"`
	Backend        string `json:"backend"`
	BackendName    string `json:"backend_name"`
	ActiveClients  int    `json:"active_clients"`
	Requests       int64  `json:"requests"`
	AvgResponseMS float64 `json:"avg_response_ms"`
	Status4XX      int64  `json:"status_4xx"`
	Status5XX      int64  `json:"status_5xx"`
}

type Response struct {
	GeneratedAt       string           `json:"generated_at"`
	Period            string           `json:"period"`
	ServiceFilter     string           `json:"service_filter,omitempty"`
	PortFilter        string           `json:"port_filter,omitempty"`
	MetricsPath       string           `json:"metrics_path"`
	PartialWindow     bool             `json:"partial_window"`
	Summary           Summary          `json:"summary"`
	Services          []ServiceSummary `json:"services"`
	Backends          []BackendSummary `json:"backends"`
	History           []HistoryPoint   `json:"history"`
	AvailableServices []string         `json:"available_services"`
	AvailablePorts    []string         `json:"available_ports"`
}

type aggregate struct {
	requests     int64
	responseSum  float64
	responseN    int64
	connectSum   float64
	connectN     int64
	status4xx    int64
	status5xx    int64
	bytes        int64
	clients      map[string]struct{}
	active       int
}

type historyAggregate struct {
	requests    int64
	responseSum float64
	responseN   int64
	status4xx   int64
	status5xx   int64
	clients     map[string]struct{}
}

type backendMeta struct {
	Name      string
	HealthKey string
	Upstreams []string
}

var aliasCache struct {
	sync.Mutex
	at      time.Time
	aliases map[string]backendMeta
}

func InitRouter(r *gin.RouterGroup) {
	r.GET("/bma/metrics", GetMetrics)
}

func metricsPath() string {
	if path := strings.TrimSpace(os.Getenv("NGINX_UI_BMA_METRICS_PATH")); path != "" {
		return path
	}
	return defaultMetricsPath
}

func GetMetrics(c *gin.Context) {
	periodName := strings.TrimSpace(c.DefaultQuery("period", "5m"))
	duration, bucket, maxBytes, ok := parsePeriod(periodName)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid period; use 1m, 5m, 15m, 1h, 6h or 24h"})
		return
	}

	serviceFilter := normalizeService(c.Query("service"))
	portFilter := strings.TrimSpace(c.Query("port"))
	path := metricsPath()
	cutoff := time.Now().Add(-duration)

	entries, partial, err := readWindow(path, cutoff, maxBytes)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, os.ErrNotExist) {
			status = http.StatusServiceUnavailable
		}
		c.JSON(status, gin.H{
			"message": "unable to read BMA load-balancing metrics",
			"error":   err.Error(),
		})
		return
	}

	response := aggregateEntries(entries, cutoff, duration, bucket, periodName, path, serviceFilter, portFilter, partial)
	c.JSON(http.StatusOK, response)
}

func parsePeriod(value string) (time.Duration, time.Duration, int64, bool) {
	switch value {
	case "1m":
		return time.Minute, time.Minute, 8 * 1024 * 1024, true
	case "5m":
		return 5 * time.Minute, time.Minute, 32 * 1024 * 1024, true
	case "15m":
		return 15 * time.Minute, time.Minute, 64 * 1024 * 1024, true
	case "1h":
		return time.Hour, 5 * time.Minute, 128 * 1024 * 1024, true
	case "6h":
		return 6 * time.Hour, 5 * time.Minute, 256 * 1024 * 1024, true
	case "24h":
		return 24 * time.Hour, 15 * time.Minute, 512 * 1024 * 1024, true
	default:
		return 0, 0, 0, false
	}
}

func readWindow(path string, cutoff time.Time, maxBytes int64) ([]logEntry, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, false, err
	}

	start := int64(0)
	if info.Size() > maxBytes {
		start = info.Size() - maxBytes
		if _, err = f.Seek(start, io.SeekStart); err != nil {
			return nil, false, err
		}

	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), maxScannerToken)

	// If we sought into the middle of the file, discard one partial line.
	if start > 0 && scanner.Scan() {
		// Intentionally ignored.
	}

	entries := make([]logEntry, 0, 4096)
	var firstTimestamp time.Time

	for scanner.Scan() {
		var entry logEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}

		ts, err := time.Parse(time.RFC3339, entry.Timestamp)
		if err != nil {
			continue
		}
		if firstTimestamp.IsZero() {
			firstTimestamp = ts
		}
		if ts.Before(cutoff) {
			continue
		}

		entries = append(entries, entry)
	}

	if err := scanner.Err(); err != nil {
		return nil, false, err
	}

	partial := start > 0 && (firstTimestamp.IsZero() || firstTimestamp.After(cutoff))
	return entries, partial, nil
}

func aggregateEntries(entries []logEntry, cutoff time.Time, duration, bucket time.Duration, periodName, path, serviceFilter, portFilter string, partial bool) Response {
	global := newAggregate()
	services := make(map[string]*aggregate)
	backends := make(map[string]*aggregate)
	assignments := make(map[string]string)
	history := make(map[string]*historyAggregate)
	availableServices := make(map[string]struct{})
	availablePorts := make(map[string]struct{})

	for _, entry := range entries {
		ts, err := time.Parse(time.RFC3339, entry.Timestamp)
		if err != nil || ts.Before(cutoff) {
			continue
		}

		service := normalizeService(entry.Host)
		if service == "" {
			service = "unknown"
		}
		availableServices[service] = struct{}{}

		backend := lastValue(entry.Upstream)
		if backend == "" || backend == "-" {
			continue
		}
		port := backendPort(backend)
		if port != "" && (serviceFilter == "" || service == serviceFilter) {
			availablePorts[port] = struct{}{}
		}

		if serviceFilter != "" && service != serviceFilter {
			continue
		}
		if portFilter != "" && port != portFilter {
			continue
		}

		if _, exists := services[service]; !exists {
			services[service] = newAggregate()
		}
		backendKey := service + "\x00" + backend
		if _, exists := backends[backendKey]; !exists {
			backends[backendKey] = newAggregate()
		}

		status := parseInt(lastValue(entry.UpstreamStatus))
		responseSeconds, responseOK := parseFloat(lastValue(entry.UpstreamResponse))
		connectSeconds, connectOK := parseFloat(lastValue(entry.UpstreamConnect))
		bytes := parseInt64(entry.Bytes)

		updateAggregate(global, entry.Client, status, responseSeconds, responseOK, connectSeconds, connectOK, bytes)
		updateAggregate(services[service], entry.Client, status, responseSeconds, responseOK, connectSeconds, connectOK, bytes)
		updateAggregate(backends[backendKey], entry.Client, status, responseSeconds, responseOK, connectSeconds, connectOK, bytes)

		if entry.Client != "" {
			assignments[service+"\x00"+entry.Client] = backend
		}

		bucketUnix := (ts.Unix() / int64(bucket.Seconds())) * int64(bucket.Seconds())
		historyKey := strconv.FormatInt(bucketUnix, 10) + "\x00" + service + "\x00" + backend
		h := history[historyKey]
		if h == nil {
			h = &historyAggregate{clients: make(map[string]struct{})}
			history[historyKey] = h
		}
		h.requests++
		if entry.Client != "" {
			h.clients[entry.Client] = struct{}{}
		}
		if responseOK {
			h.responseSum += responseSeconds
			h.responseN++
		}
		if status >= 400 && status < 500 {
			h.status4xx++
		}
		if status >= 500 {
			h.status5xx++
		}
	}

	for key, backend := range assignments {
		parts := strings.SplitN(key, "\x00", 2)
		if len(parts) != 2 {
			continue
		}
		service := parts[0]
		backendKey := service + "\x00" + backend
		if b := backends[backendKey]; b != nil {
			b.active++
		}
	}

	aliases := getBackendAliases()
	health := upstreamsvc.GetUpstreamService().GetAvailabilityMap()

	response := Response{
		GeneratedAt:       time.Now().Format(time.RFC3339),
		Period:            periodName,
		ServiceFilter:     serviceFilter,
		PortFilter:        portFilter,
		MetricsPath:       path,
		PartialWindow:     partial,
		Summary:           toSummary(global, duration),
		AvailableServices: sortedKeys(availableServices),
		AvailablePorts:    sortedPorts(availablePorts),
	}

	serviceNames := make([]string, 0, len(services))
	for service := range services {
		serviceNames = append(serviceNames, service)
	}
	sort.Strings(serviceNames)
	for _, service := range serviceNames {
		response.Services = append(response.Services, ServiceSummary{
			Service: service,
			Summary: toSummary(services[service], duration),
		})
	}

	backendKeys := make([]string, 0, len(backends))
	for key := range backends {
		backendKeys = append(backendKeys, key)
	}
	sort.Strings(backendKeys)
	for _, key := range backendKeys {
		parts := strings.SplitN(key, "\x00", 2)
		if len(parts) != 2 {
			continue
		}
		service, backend := parts[0], parts[1]
		state := backends[key]
		serviceClients := len(services[service].clients)
		percent := 0.0
		if serviceClients > 0 {
			percent = float64(state.active) / float64(serviceClients) * 100
		}

		meta := aliases[backend]
		name := backend
		if meta.Name != "" {
			name = meta.Name
		}

		item := BackendSummary{
			Service:        service,
			Backend:        backend,
			BackendName:    name,
			Port:           backendPort(backend),
			Upstreams:      append([]string(nil), meta.Upstreams...),
			ActiveClients:  state.active,
			ClientPercent:  percent,
			Requests:       state.requests,
			RequestsPerS:   safeRate(state.requests, duration),
			AvgResponseMS:  averageMS(state.responseSum, state.responseN),
			AvgConnectMS:   averageMS(state.connectSum, state.connectN),
			Status4XX:      state.status4xx,
			Status5XX:      state.status5xx,
			Bytes:          state.bytes,
		}

		if meta.HealthKey != "" {
			if status, ok := health[meta.HealthKey]; ok && status != nil {
				online := status.Online
				item.Online = &online
				item.HealthLatencyMS = float64(status.Latency)
			}
		}
		response.Backends = append(response.Backends, item)
	}

	historyKeys := make([]string, 0, len(history))
	for key := range history {
		historyKeys = append(historyKeys, key)
	}
	sort.Slice(historyKeys, func(i, j int) bool {
		return historyKeys[i] < historyKeys[j]
	})
	for _, key := range historyKeys {
		parts := strings.SplitN(key, "\x00", 3)
		if len(parts) != 3 {
			continue
		}
		bucketUnix, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			continue
		}
		service, backend := parts[1], parts[2]
		h := history[key]
		name := backend
		if meta := aliases[backend]; meta.Name != "" {
			name = meta.Name
		}
		response.History = append(response.History, HistoryPoint{
			Timestamp:      time.Unix(bucketUnix, 0).Local().Format(time.RFC3339),
			Service:        service,
			Backend:        backend,
			BackendName:    name,
			ActiveClients:  len(h.clients),
			Requests:       h.requests,
			AvgResponseMS: averageMS(h.responseSum, h.responseN),
			Status4XX:      h.status4xx,
			Status5XX:      h.status5xx,
		})
	}

	return response
}

func newAggregate() *aggregate {
	return &aggregate{clients: make(map[string]struct{})}
}

func updateAggregate(a *aggregate, client string, status int, response float64, responseOK bool, connect float64, connectOK bool, bytes int64) {
	a.requests++
	if client != "" {
		a.clients[client] = struct{}{}
	}
	if responseOK {
		a.responseSum += response
		a.responseN++
	}
	if connectOK {
		a.connectSum += connect
		a.connectN++
	}
	if status >= 400 && status < 500 {
		a.status4xx++
	}
	if status >= 500 {
		a.status5xx++
	}
	a.bytes += bytes
}

func toSummary(a *aggregate, duration time.Duration) Summary {
	return Summary{
		ActiveClients: len(a.clients),
		Requests:      a.requests,
		RequestsPerS:  safeRate(a.requests, duration),
		AvgResponseMS: averageMS(a.responseSum, a.responseN),
		AvgConnectMS:  averageMS(a.connectSum, a.connectN),
		Status4XX:     a.status4xx,
		Status5XX:     a.status5xx,
		Bytes:         a.bytes,
	}
}

func safeRate(requests int64, duration time.Duration) float64 {
	if duration <= 0 {
		return 0
	}
	return float64(requests) / duration.Seconds()
}

func averageMS(sum float64, count int64) float64 {
	if count == 0 {
		return 0
	}
	return sum / float64(count) * 1000
}

func lastValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	parts := strings.Split(value, ",")
	return strings.TrimSpace(parts[len(parts)-1])
}

func parseFloat(value string) (float64, bool) {
	value = strings.TrimSpace(value)
	if value == "" || value == "-" {
		return 0, false
	}
	v, err := strconv.ParseFloat(value, 64)
	return v, err == nil
}

func parseInt(value string) int {
	v, _ := strconv.Atoi(strings.TrimSpace(value))
	return v
}

func parseInt64(value string) int64 {
	v, _ := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	return v
}

func normalizeService(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	host = strings.TrimSuffix(host, ".")
	host = strings.TrimSuffix(host, ".bancoademi.local")
	return host
}

func backendPort(backend string) string {
	if host, port, err := net.SplitHostPort(backend); err == nil && host != "" {
		return port
	}
	idx := strings.LastIndex(backend, ":")
	if idx < 0 || idx == len(backend)-1 {
		return ""
	}
	return backend[idx+1:]
}

func sortedKeys(values map[string]struct{}) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func sortedPorts(values map[string]struct{}) []string {
	out := sortedKeys(values)
	sort.Slice(out, func(i, j int) bool {
		pi, ei := strconv.Atoi(out[i])
		pj, ej := strconv.Atoi(out[j])
		if ei == nil && ej == nil {
			return pi < pj
		}
		return out[i] < out[j]
	})
	return out
}

func getBackendAliases() map[string]backendMeta {
	aliasCache.Lock()
	defer aliasCache.Unlock()

	if aliasCache.aliases != nil && time.Since(aliasCache.at) < 5*time.Minute {
		return aliasCache.aliases
	}

	service := upstreamsvc.GetUpstreamService()
	targets := service.GetTargetInfos()
	definitions := service.GetAllUpstreamDefinitions()

	upstreamsByTarget := make(map[string][]string)
	for name, definition := range definitions {
		for _, server := range definition.Servers {
			socket := joinSocket(server.Host, server.Port)
			upstreamsByTarget[socket] = appendUnique(upstreamsByTarget[socket], name)
		}
	}
	for socket := range upstreamsByTarget {
		sort.Strings(upstreamsByTarget[socket])
	}

	aliases := make(map[string]backendMeta)
	for _, target := range targets {
		socket := joinSocket(target.Host, target.Port)
		name := strings.ToUpper(shortHostname(target.Host))
		if target.Port != "" {
			name += ":" + target.Port
		}
		meta := backendMeta{
			Name:      name,
			HealthKey: socket,
			Upstreams: append([]string(nil), upstreamsByTarget[socket]...),
		}
		aliases[socket] = meta

		if net.ParseIP(strings.Trim(target.Host, "[]")) != nil {
			continue
		}

		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		addresses, err := net.DefaultResolver.LookupHost(ctx, target.Host)
		cancel()
		if err != nil {
			continue
		}
		for _, address := range addresses {
			aliases[joinSocket(address, target.Port)] = meta
		}
	}

	aliasCache.aliases = aliases
	aliasCache.at = time.Now()
	return aliases
}

func shortHostname(host string) string {
	host = strings.Trim(host, "[]")
	if idx := strings.Index(host, "."); idx > 0 {
		return host[:idx]
	}
	return host
}

func joinSocket(host, port string) string {
	host = strings.TrimSpace(host)
	port = strings.TrimSpace(port)
	if port == "" {
		return host
	}
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		return "[" + host + "]:" + port
	}
	return host + ":" + port
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}
