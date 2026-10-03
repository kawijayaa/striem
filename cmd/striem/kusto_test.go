package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeKustoConfigurationAndTokenRotation(t *testing.T) {
	for _, name := range []string{"STRIEM_KUSTO_ENDPOINT", "STRIEM_KUSTO_DATABASE", "STRIEM_KUSTO_TOKEN_FILE", "STRIEM_KUSTO_MANAGEMENT"} {
		t.Setenv(name, "")
	}
	config, err := nativeKustoFromEnv()
	if err != nil || config.Client != nil {
		t.Fatal("default enabled native Kusto")
	}
	t.Setenv("STRIEM_KUSTO_ENDPOINT", "https://cluster.example")
	if _, err := nativeKustoFromEnv(); err == nil {
		t.Fatal("accepted partial configuration")
	}
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte("first-token\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("STRIEM_KUSTO_TOKEN_FILE", path)
	t.Setenv("STRIEM_KUSTO_DATABASE", "Telemetry")
	config, err = nativeKustoFromEnv()
	if err != nil || config.Client == nil || config.AllowManagement || config.Database != "Telemetry" {
		t.Fatalf("invalid config: %+v %v", config, err)
	}
	t.Setenv("STRIEM_KUSTO_MANAGEMENT", "true")
	config, err = nativeKustoFromEnv()
	if err != nil || !config.AllowManagement {
		t.Fatal("management opt-in lost")
	}
	if token, err := readKustoToken(path); err != nil || token != "first-token" {
		t.Fatalf("invalid token read: %v", err)
	}
	if err := os.WriteFile(path, []byte("second-token\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if token, err := readKustoToken(path); err != nil || token != "second-token" {
		t.Fatalf("token rotation lost: %v", err)
	}
	t.Setenv("STRIEM_KUSTO_MANAGEMENT", "not-a-bool")
	if _, err := nativeKustoFromEnv(); err == nil {
		t.Fatal("accepted bad management opt-in")
	}
	t.Setenv("STRIEM_KUSTO_MANAGEMENT", "false")
	t.Setenv("STRIEM_KUSTO_ENDPOINT", "http://cluster.example")
	if _, err := nativeKustoFromEnv(); err == nil {
		t.Fatal("allowed plaintext service credentials")
	}
}

func TestNativeTokenFileFailures(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	if _, err := readKustoToken(path); err == nil {
		t.Fatal("missing token file accepted")
	}
	for _, text := range []string{"", " \n", "secret\nheader", strings.Repeat("x", (64<<10)+1), "secret\x00"} {
		if err := os.WriteFile(path, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := readKustoToken(path); err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatalf("bad token accepted/leaked: %v", err)
		}
	}
	if _, err := readKustoToken(t.TempDir()); err == nil {
		t.Fatal("nonregular file accepted")
	}
}
