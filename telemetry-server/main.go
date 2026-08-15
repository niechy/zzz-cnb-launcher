package main

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	maxRequestBytes = 64 * 1024
	maxEvents       = 50
	requestsPerMin  = 60
)

var (
	installIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)
	versionPattern   = regexp.MustCompile(`^v?[0-9]+(?:\.[0-9]+){1,3}(?:-[0-9A-Za-z.-]+)?$`)
	namePattern      = regexp.MustCompile(`^[a-z][a-z0-9_]{0,47}$`)
	valuePattern     = regexp.MustCompile(`^[A-Za-z0-9_.:-]{0,64}$`)
)

type batch struct {
	Events []event `json:"events"`
}

type event struct {
	Schema     int    `json:"schema"`
	InstallID  string `json:"install_id"`
	AppVersion string `json:"app_version"`
	Name       string `json:"name"`
	Stage      string `json:"stage,omitempty"`
	Result     string `json:"result"`
	ErrorCode  string `json:"error_code,omitempty"`
	DurationMS int64  `json:"duration_ms,omitempty"`
	Channel    string `json:"channel,omitempty"`
	ClientTime string `json:"client_time,omitempty"`
	ReceivedAt string `json:"received_at"`
}

type writer struct {
	dir       string
	mu        sync.Mutex
	lastPrune time.Time
}

func (w *writer) append(events []event) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := os.MkdirAll(w.dir, 0700); err != nil {
		return err
	}
	if w.lastPrune.IsZero() || time.Since(w.lastPrune) >= 24*time.Hour {
		w.prune(90 * 24 * time.Hour)
		w.lastPrune = time.Now()
	}
	path := filepath.Join(w.dir, "events-"+time.Now().UTC().Format("2006-01-02")+".jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(f)
	for _, item := range events {
		if err := encoder.Encode(item); err != nil {
			_ = f.Close()
			return err
		}
	}
	return f.Close()
}

func (w *writer) prune(retention time.Duration) {
	entries, err := os.ReadDir(w.dir)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-retention)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "events-") || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		info, err := entry.Info()
		if err == nil && info.ModTime().Before(cutoff) {
			_ = os.Remove(filepath.Join(w.dir, entry.Name()))
		}
	}
}

type rateEntry struct {
	window time.Time
	count  int
}

type limiter struct {
	mu      sync.Mutex
	clients map[string]rateEntry
}

func (l *limiter) allow(ip string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	entry := l.clients[ip]
	if entry.window.IsZero() || now.Sub(entry.window) >= time.Minute {
		l.clients[ip] = rateEntry{window: now, count: 1}
		return true
	}
	if entry.count >= requestsPerMin {
		return false
	}
	entry.count++
	l.clients[ip] = entry
	if len(l.clients) > 10000 {
		for key, value := range l.clients {
			if now.Sub(value.window) >= 2*time.Minute {
				delete(l.clients, key)
			}
		}
	}
	return true
}

func main() {
	tlsListen := flag.String("listen", ":8443", "TLS listen address")
	httpListen := flag.String("http-listen", ":8080", "plain HTTP listen address")
	cert := flag.String("cert", "/opt/zzz-launcher-telemetry/cert/server.crt", "TLS certificate")
	key := flag.String("key", "/opt/zzz-launcher-telemetry/cert/server.key", "TLS private key")
	data := flag.String("data", "/opt/zzz-launcher-telemetry/data", "event data directory")
	flag.Parse()

	eventWriter := &writer{dir: *data}
	rateLimiter := &limiter{clients: make(map[string]rateEntry)}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		response.Header().Set("Cache-Control", "no-store")
		_, _ = io.WriteString(response, `{"status":"ok"}`)
	})
	mux.HandleFunc("POST /v1/events", func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Cache-Control", "no-store")
		ip, _, err := net.SplitHostPort(request.RemoteAddr)
		if err != nil {
			ip = request.RemoteAddr
		}
		if !rateLimiter.allow(ip, time.Now()) {
			http.Error(response, "rate limit", http.StatusTooManyRequests)
			return
		}
		request.Body = http.MaxBytesReader(response, request.Body, maxRequestBytes)
		decoder := json.NewDecoder(request.Body)
		decoder.DisallowUnknownFields()
		var payload batch
		if err := decoder.Decode(&payload); err != nil {
			http.Error(response, "invalid json", http.StatusBadRequest)
			return
		}
		if err := ensureEOF(decoder); err != nil {
			http.Error(response, "invalid json", http.StatusBadRequest)
			return
		}
		if len(payload.Events) == 0 || len(payload.Events) > maxEvents {
			http.Error(response, "invalid event count", http.StatusBadRequest)
			return
		}
		receivedAt := time.Now().UTC().Format(time.RFC3339Nano)
		for index := range payload.Events {
			if err := validateEvent(payload.Events[index]); err != nil {
				http.Error(response, "invalid event", http.StatusBadRequest)
				return
			}
			payload.Events[index].ReceivedAt = receivedAt
		}
		if err := eventWriter.append(payload.Events); err != nil {
			log.Printf("event write failed: %v", err)
			http.Error(response, "storage unavailable", http.StatusServiceUnavailable)
			return
		}
		response.WriteHeader(http.StatusAccepted)
	})

	handler := securityHeaders(mux)
	httpServer := &http.Server{
		Addr:              *httpListen,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       8 * time.Second,
		WriteTimeout:      8 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    16 * 1024,
	}
	tlsServer := &http.Server{
		Addr:              *tlsListen,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       8 * time.Second,
		WriteTimeout:      8 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    16 * 1024,
		TLSConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
			NextProtos: []string{"http/1.1"},
		},
	}
	errors := make(chan error, 2)
	go func() {
		log.Printf("telemetry HTTP collector listening on %s", *httpListen)
		errors <- httpServer.ListenAndServe()
	}()
	go func() {
		log.Printf("telemetry HTTPS collector listening on %s", *tlsListen)
		errors <- tlsServer.ListenAndServeTLS(*cert, *key)
	}()
	log.Fatal(<-errors)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("X-Content-Type-Options", "nosniff")
		response.Header().Set("Referrer-Policy", "no-referrer")
		response.Header().Set("Content-Security-Policy", "default-src 'none'")
		next.ServeHTTP(response, request)
	})
}

func ensureEOF(decoder *json.Decoder) error {
	var extra any
	err := decoder.Decode(&extra)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err == nil {
		return errors.New("extra json value")
	}
	return err
}

func validateEvent(item event) error {
	if item.Schema != 1 || !installIDPattern.MatchString(item.InstallID) || !versionPattern.MatchString(item.AppVersion) {
		return errors.New("invalid identity")
	}
	if !namePattern.MatchString(item.Name) || !validOptionalName(item.Stage) {
		return errors.New("invalid name")
	}
	if item.Result != "success" && item.Result != "failure" && item.Result != "started" && item.Result != "skipped" {
		return errors.New("invalid result")
	}
	if !valuePattern.MatchString(item.ErrorCode) || !valuePattern.MatchString(item.Channel) {
		return errors.New("invalid value")
	}
	if item.DurationMS < 0 || item.DurationMS > int64((24*time.Hour)/time.Millisecond) {
		return errors.New("invalid duration")
	}
	if item.ClientTime != "" {
		parsed, err := time.Parse(time.RFC3339, item.ClientTime)
		if err != nil || time.Since(parsed) > 30*24*time.Hour || time.Until(parsed) > 24*time.Hour {
			return errors.New("invalid client time")
		}
	}
	return nil
}

func validOptionalName(value string) bool {
	return value == "" || namePattern.MatchString(value)
}

func init() {
	log.SetFlags(log.Ldate | log.Ltime | log.LUTC)
	log.SetPrefix("collector: ")
}
