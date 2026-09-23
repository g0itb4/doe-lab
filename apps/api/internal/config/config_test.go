package config

import (
	"strings"
	"testing"
	"time"
)

// production is an environment that Load accepts in production.
var production = map[string]string{
	"DOELAB_ENV":           "production",
	"ENGINE_TOKEN":         "engine-token-0123456789abcdef",
	"OPERATOR_TOKEN":       "operator-token-0123456789abcdef",
	"DEVICE_TOKEN_SECRET":  "device-secret-0123456789abcdef",
	"S3_SECRET_ACCESS_KEY": "spaces-secret-0123456789abcdef",
}

// allKeys is every variable Load reads; each test starts with them empty so the
// developer's own shell cannot leak in.
var allKeys = []string{
	"DOELAB_ENV", "DATABASE_URL", "HOST", "PORT", "DOELAB_VERSION", "API_URL", "METRICS_ADDR",
	"OTEL_EXPORTER_OTLP_ENDPOINT", "ANTHROPIC_API_KEY", "DB_MAX_CONNS", "REQUEST_TIMEOUT_MS",
	"S3_ENDPOINT", "S3_REGION", "S3_BUCKET", "S3_ACCESS_KEY_ID", "S3_SECRET_ACCESS_KEY", "S3_PATH_STYLE",
	"ENGINE_TOKEN", "OPERATOR_TOKEN", "DEVICE_TOKEN_SECRET", "DEMO_CLOCK_SPEED", "DEMO_CLOCK_ANCHOR",
}

func setEnv(t *testing.T, vars ...map[string]string) {
	t.Helper()
	for _, k := range allKeys {
		t.Setenv(k, "")
	}
	for _, m := range vars {
		for k, v := range m {
			t.Setenv(k, v)
		}
	}
}

func TestLoadDevelopment(t *testing.T) {
	setEnv(t, map[string]string{"DOELAB_ENV": "development"})
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Env != Development || !c.Reflection || !c.LogRequests || c.TrustProxy {
		t.Errorf("development posture = %+v", c)
	}
	if c.Port != 3100 || c.Host != "127.0.0.1" || c.DBMaxConns != 16 || c.RequestTimeout != 15*time.Second {
		t.Errorf("defaults = %+v", c)
	}
	if c.DatabaseURL != defaultDatabaseURL || c.EngineToken != devEngineToken || c.OperatorToken != devOperatorToken {
		t.Errorf("development credentials = %+v", c)
	}
	if !c.S3.PathStyle || c.S3.Bucket != "doelab" || c.S3.Endpoint != "http://localhost:9200" {
		t.Errorf("S3 = %+v", c.S3)
	}
	if c.AnthropicAPIKey != "" || c.OTLPEndpoint != "" {
		t.Errorf("optional features on by default: %+v", c)
	}
}

func TestLoadTestCountsAsDevelopment(t *testing.T) {
	setEnv(t, map[string]string{"DOELAB_ENV": " Test "})
	c, err := Load()
	if err != nil || c.Env != Development {
		t.Errorf("env = %v, error %v", c.Env, err)
	}
}

func TestLoadProduction(t *testing.T) {
	setEnv(t, production, map[string]string{
		"PORT": "8443", "DB_MAX_CONNS": "32", "REQUEST_TIMEOUT_MS": "5000", "S3_PATH_STYLE": "false",
		"DOELAB_VERSION": "abc123", "S3_ENDPOINT": "https://syd1.digitaloceanspaces.com",
	})
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Env != Production || c.Reflection || c.LogRequests || !c.TrustProxy || c.S3.PathStyle {
		t.Errorf("production posture = %+v", c)
	}
	if c.Port != 8443 || c.DBMaxConns != 32 || c.RequestTimeout != 5*time.Second || c.Version != "abc123" {
		t.Errorf("overrides = %+v", c)
	}
}

// With nothing set, the process is in production and has no credentials: it
// must refuse to start rather than run on the defaults printed in
// .env.example.
func TestLoadUnsetIsProductionAndFailsClosed(t *testing.T) {
	setEnv(t)
	c, err := Load()
	if c.Env != Production {
		t.Errorf("env = %v, want production", c.Env)
	}
	if err == nil || !strings.Contains(err.Error(), "production refuses the development default") {
		t.Errorf("error = %v", err)
	}
}

func TestLoadErrors(t *testing.T) {
	tests := []struct {
		name string
		vars map[string]string
		want string
	}{
		{name: "unknown environment", vars: map[string]string{"DOELAB_ENV": "prod"}, want: `DOELAB_ENV: expected production, development or test, got "prod"`},
		{name: "bad port", vars: map[string]string{"DOELAB_ENV": "development", "PORT": "http"}, want: `PORT: expected an integer, got "http"`},
		{name: "bad pool size", vars: map[string]string{"DOELAB_ENV": "development", "DB_MAX_CONNS": "many"}, want: "DB_MAX_CONNS: expected an integer"},
		{name: "pool size out of range", vars: map[string]string{"DOELAB_ENV": "development", "DB_MAX_CONNS": "0"}, want: "DB_MAX_CONNS: expected 1 to 1000, got 0"},
		{name: "bad timeout", vars: map[string]string{"DOELAB_ENV": "development", "REQUEST_TIMEOUT_MS": "1s"}, want: "REQUEST_TIMEOUT_MS: expected an integer"},
		{name: "bad clock speed", vars: map[string]string{"DOELAB_ENV": "development", "DEMO_CLOCK_SPEED": "fast"}, want: `DEMO_CLOCK_SPEED: expected a number, got "fast"`},
		{name: "fast clock with no anchor", vars: map[string]string{"DOELAB_ENV": "development", "DEMO_CLOCK_SPEED": "60"}, want: "DEMO_CLOCK_ANCHOR: required when DEMO_CLOCK_SPEED is not 1"},
		{name: "bad clock anchor", vars: map[string]string{"DOELAB_ENV": "development", "DEMO_CLOCK_ANCHOR": "yesterday"}, want: `DEMO_CLOCK_ANCHOR: expected an RFC 3339 time, got "yesterday"`},
		{name: "bad path style", vars: map[string]string{"DOELAB_ENV": "development", "S3_PATH_STYLE": "maybe"}, want: `S3_PATH_STYLE: expected true or false, got "maybe"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setEnv(t, tt.vars)
			_, err := Load()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %v, want one containing %q", err, tt.want)
			}
		})
	}
}

func TestProductionRefusesWeakSecrets(t *testing.T) {
	tests := []struct {
		name string
		vars map[string]string
		want string
	}{
		{name: "development engine token", vars: map[string]string{"ENGINE_TOKEN": devEngineToken}, want: "ENGINE_TOKEN: production refuses the development default"},
		{name: "development operator token", vars: map[string]string{"OPERATOR_TOKEN": devOperatorToken}, want: "OPERATOR_TOKEN: production refuses"},
		{name: "development device secret", vars: map[string]string{"DEVICE_TOKEN_SECRET": devDeviceSecret}, want: "DEVICE_TOKEN_SECRET: production refuses"},
		{name: "development S3 secret", vars: map[string]string{"S3_SECRET_ACCESS_KEY": devS3SecretKey}, want: "S3_SECRET_ACCESS_KEY: production refuses"},
		{name: "short token", vars: map[string]string{"ENGINE_TOKEN": "short"}, want: "ENGINE_TOKEN: production needs at least 24 characters"},
		{name: "one token for two scopes", vars: map[string]string{"ENGINE_TOKEN": production["OPERATOR_TOKEN"]}, want: "ENGINE_TOKEN and OPERATOR_TOKEN must differ"},
		{name: "metrics off loopback", vars: map[string]string{"METRICS_ADDR": "0.0.0.0:9464"}, want: "METRICS_ADDR: production serves metrics on loopback only"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setEnv(t, production, tt.vars)
			_, err := Load()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %v, want one containing %q", err, tt.want)
			}
		})
	}

	t.Run("localhost metrics are accepted", func(t *testing.T) {
		setEnv(t, production, map[string]string{"METRICS_ADDR": "localhost:9464"})
		if _, err := Load(); err != nil {
			t.Error(err)
		}
	})
}

func TestLoadClock(t *testing.T) {
	setEnv(t, map[string]string{"DOELAB_ENV": "development"})
	c, err := Load()
	if err != nil || c.ClockSpeed != 1 || c.ClockAnchor.IsZero() {
		t.Errorf("default clock = speed %v, anchor %v, %v", c.ClockSpeed, c.ClockAnchor, err)
	}

	setEnv(t, map[string]string{"DOELAB_ENV": "development", "DEMO_CLOCK_SPEED": "60", "DEMO_CLOCK_ANCHOR": "2026-10-01T00:00:00+10:00"})
	c, err = Load()
	if err != nil || c.ClockSpeed != 60 || !c.ClockAnchor.Equal(time.Date(2026, 9, 30, 14, 0, 0, 0, time.UTC)) {
		t.Errorf("demo clock = speed %v, anchor %v, %v", c.ClockSpeed, c.ClockAnchor, err)
	}
}
