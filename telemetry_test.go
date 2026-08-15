package main

import (
	"crypto/tls"
	"errors"
	"os"
	"testing"
	"time"
)

func TestTelemetryDisabledDoesNotInitialize(t *testing.T) {
	client := newTelemetryClient(true)
	if client.enabled || client.http != nil || client.tls != nil || client.installID != "" {
		t.Fatal("disabled telemetry must not initialize persistent identity or networking")
	}
}

func TestTelemetryErrorCodeIsCategorical(t *testing.T) {
	err := errors.New(`open C:\\Users\\Alice\\secret-token.txt: access is denied`)
	code := telemetryErrorCode(err)
	if code != "unknown" {
		t.Fatalf("unexpected category: %q", code)
	}
	if code == err.Error() {
		t.Fatal("raw error text must never be returned")
	}
}

func TestTelemetryPinRejectsMissingCertificate(t *testing.T) {
	if err := verifyTelemetryConnection(tls.ConnectionState{}); err == nil {
		t.Fatal("connection without a certificate must be rejected")
	}
}

func TestTelemetryEndpointIntegration(t *testing.T) {
	if testing.Short() || !envEnabled("ZZZ_TELEMETRY_INTEGRATION") {
		t.Skip("set ZZZ_TELEMETRY_INTEGRATION=1 to test the deployed collector")
	}
	client := newTelemetryClient(false)
	if !client.enabled {
		t.Fatal("telemetry client did not initialize")
	}
	event := telemetryEvent{
		Schema:     1,
		InstallID:  "fedcba9876543210fedcba9876543210",
		AppVersion: appVersion,
		Name:       "integration_test",
		Stage:      "test",
		Result:     "success",
		ClientTime: time.Now().UTC().Format(time.RFC3339),
	}
	if err := client.send(event); err != nil {
		t.Fatal(err)
	}
}

func envEnabled(name string) bool {
	value, ok := os.LookupEnv(name)
	return ok && value == "1"
}
