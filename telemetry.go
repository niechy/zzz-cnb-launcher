package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	telemetryEndpoint    = "http://47.104.196.213:8080/v1/events"
	tlsTelemetryEndpoint = "https://47.104.196.213:8443/v1/events"
	telemetrySPKI        = "eea36d843886ff64a0bca15e2c0bbcc9411846a29fa5aee634e7adb9863474e6"
)

type telemetryEvent struct {
	Schema     int    `json:"schema"`
	InstallID  string `json:"install_id"`
	AppVersion string `json:"app_version"`
	Name       string `json:"name"`
	Stage      string `json:"stage,omitempty"`
	Result     string `json:"result"`
	ErrorCode  string `json:"error_code,omitempty"`
	DurationMS int64  `json:"duration_ms,omitempty"`
	Channel    string `json:"channel,omitempty"`
	ClientTime string `json:"client_time"`
}

type telemetryBatch struct {
	Events []telemetryEvent `json:"events"`
}

type telemetryClient struct {
	enabled   bool
	installID string
	http      *http.Client
	tls       *http.Client
	wait      sync.WaitGroup
}

func newTelemetryClient(disabled bool) *telemetryClient {
	client := &telemetryClient{}
	if disabled || telemetryMarkerExists() {
		return client
	}
	installID, err := loadOrCreateInstallID()
	if err != nil {
		return client
	}
	dialer := &net.Dialer{Timeout: 1500 * time.Millisecond, KeepAlive: 15 * time.Second}
	httpTransport := &http.Transport{
		Proxy:             nil,
		DialContext:       dialer.DialContext,
		DisableKeepAlives: true,
	}
	tlsTransport := &http.Transport{
		Proxy:               nil,
		DialContext:         dialer.DialContext,
		TLSHandshakeTimeout: 1500 * time.Millisecond,
		DisableKeepAlives:   true,
		TLSClientConfig: &tls.Config{
			MinVersion:         tls.VersionTLS12,
			InsecureSkipVerify: true, // Trust is established by the SPKI pin below.
			VerifyConnection:   verifyTelemetryConnection,
		},
	}
	client.enabled = true
	client.installID = installID
	client.http = &http.Client{Transport: httpTransport, Timeout: 2500 * time.Millisecond}
	client.tls = &http.Client{Transport: tlsTransport, Timeout: 2500 * time.Millisecond}
	return client
}

func (client *telemetryClient) record(name, stage, result, errorCode, channel string, started time.Time) {
	if client == nil || !client.enabled {
		return
	}
	duration := int64(0)
	if !started.IsZero() {
		duration = time.Since(started).Milliseconds()
		if duration < 0 {
			duration = 0
		}
	}
	event := telemetryEvent{
		Schema:     1,
		InstallID:  client.installID,
		AppVersion: strings.TrimPrefix(appVersion, "v"),
		Name:       name,
		Stage:      stage,
		Result:     result,
		ErrorCode:  errorCode,
		DurationMS: duration,
		Channel:    channel,
		ClientTime: time.Now().UTC().Format(time.RFC3339),
	}
	client.wait.Add(1)
	go func() {
		defer client.wait.Done()
		_ = client.send(event)
	}()
}

func (client *telemetryClient) send(event telemetryEvent) error {
	plain, err := json.Marshal(telemetryBatch{Events: []telemetryEvent{event}})
	if err != nil {
		return err
	}
	if err := postTelemetry(client.http, telemetryEndpoint, plain); err == nil {
		return nil
	}
	return postTelemetry(client.tls, tlsTelemetryEndpoint, plain)
}

func postTelemetry(client *http.Client, endpoint string, body []byte) error {
	request, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "ZZZ-CNB-Launcher/"+strings.TrimPrefix(appVersion, "v"))
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return errors.New("telemetry server rejected event")
	}
	return nil
}

func (client *telemetryClient) flush(timeout time.Duration) {
	if client == nil || !client.enabled {
		return
	}
	done := make(chan struct{})
	go func() {
		client.wait.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(timeout):
	}
}

func verifyTelemetryConnection(state tls.ConnectionState) error {
	if len(state.PeerCertificates) == 0 {
		return errors.New("telemetry server did not provide a certificate")
	}
	certificate := state.PeerCertificates[0]
	now := time.Now()
	if now.Before(certificate.NotBefore) || now.After(certificate.NotAfter) {
		return errors.New("telemetry certificate is outside its validity period")
	}
	if err := certificate.VerifyHostname("47.104.196.213"); err != nil {
		return err
	}
	digest := sha256.Sum256(certificate.RawSubjectPublicKeyInfo)
	if !strings.EqualFold(hex.EncodeToString(digest[:]), telemetrySPKI) {
		return errors.New("telemetry certificate pin mismatch")
	}
	return nil
}

func telemetryMarkerExists() bool {
	executable, err := os.Executable()
	if err != nil {
		return false
	}
	return fileExists(filepath.Join(filepath.Dir(executable), "ZZZ-CNB-Launcher.telemetry-disabled"))
}

func loadOrCreateInstallID() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "ZZZ-CNB-Launcher")
	path := filepath.Join(dir, "install-id")
	if data, readErr := os.ReadFile(path); readErr == nil {
		value := strings.TrimSpace(string(data))
		if len(value) == 32 {
			if _, decodeErr := hex.DecodeString(value); decodeErr == nil {
				return strings.ToLower(value), nil
			}
		}
	}
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	id := hex.EncodeToString(value)
	if err := os.WriteFile(path, []byte(id+"\n"), 0600); err != nil {
		return "", err
	}
	return id, nil
}

func telemetryErrorCode(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, os.ErrDeadlineExceeded) {
		return "timeout"
	}
	if errors.Is(err, os.ErrPermission) {
		return "permission"
	}
	if errors.Is(err, os.ErrNotExist) {
		return "not_found"
	}
	var urlError *url.Error
	if errors.As(err, &urlError) {
		return "network"
	}
	var netError net.Error
	if errors.As(err, &netError) {
		return "network"
	}
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "sha-256"), strings.Contains(message, "checksum"), strings.Contains(message, "hash"):
		return "checksum"
	case strings.Contains(message, "http"):
		return "http"
	case strings.Contains(message, "zip"), strings.Contains(message, "archive"):
		return "archive"
	case strings.Contains(message, "config"), strings.Contains(message, "配置"):
		return "config"
	case strings.Contains(message, "启动"), strings.Contains(message, "launcher"):
		return "launch"
	default:
		return "unknown"
	}
}
