package app

import (
	"testing"
)

func TestGetEnv(t *testing.T) {
	key := "SAFEBIN_TEST_KEY"
	val := "somevalue"

	if got := getEnv(key, "default"); got != "default" {
		t.Errorf("Expected default, got %s", got)
	}

	t.Setenv(key, val)
	if got := getEnv(key, "default"); got != val {
		t.Errorf("Expected %s, got %s", val, got)
	}
}

func TestGetEnvInt(t *testing.T) {
	key := "SAFEBIN_TEST_INT"

	if got := getEnvInt(key, 8080); got != 8080 {
		t.Errorf("Expected default 8080, got %d", got)
	}

	t.Setenv(key, "9090")
	if got := getEnvInt(key, 8080); got != 9090 {
		t.Errorf("Expected 9090, got %d", got)
	}

	t.Setenv(key, "notanumber")
	if got := getEnvInt(key, 8080); got != 8080 {
		t.Errorf("Expected fallback on invalid input, got %d", got)
	}
}
