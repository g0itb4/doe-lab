// Package config turns the environment into a typed struct, once, at startup.
//
// No viper, no config files: every knob here is a single env var with a default
// that works on a developer laptop, which is the same contract .env.example
// documents.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Development defaults. They match infra/compose.yaml and are refused in
// production.
const (
	// sslmode is explicit because the drivers disagree on the default: lib/pq
	// (the migration runner) requires TLS unless told otherwise, pgx does not.
	defaultDatabaseURL = "postgres://doelab:doelab@localhost:5436/doelab?sslmode=disable"
	defaultPort        = 3100

	devEngineToken   = "dev-engine-token"
	devOperatorToken = "dev-operator-token"
	devDeviceSecret  = "dev-device-secret"
	devS3AccessKey   = "doelab"
	devS3SecretKey   = "doelab-dev-only"
)

// Environment is the one knob the hardening defaults hang off.
//
// It is parsed rather than compared inline so that an unknown value is an
// error at startup, and an ABSENT value is production: the safe end, which
// development opts out of explicitly.
type Environment string

// The environments.
const (
	Development Environment = "development"
	Production  Environment = "production"
)

// S3 locates the object store.
type S3 struct {
	Endpoint        string
	Region          string
	Bucket          string
	AccessKeyID     string
	SecretAccessKey string
	// PathStyle addresses a bucket as http://host/bucket, which the local
	// gateway needs; DigitalOcean Spaces uses virtual hosts.
	PathStyle bool
}

// Config is the whole of the runtime configuration. The API reads all of it;
// the engine and the simulator read APIURL and the tokens.
type Config struct {
	DatabaseURL string
	// Host is the bind address. Loopback by default: the h2c listener is
	// plaintext and is reachable only through the edge.
	Host    string
	Port    int
	Version string

	// RequestTimeout bounds a single unary RPC. Streams are bounded by their
	// keepalive instead.
	RequestTimeout time.Duration
	DBMaxConns     int32

	// Env decides every hardening default below. Production unless something
	// says otherwise.
	Env Environment
	// TrustProxy decides whether X-Forwarded-For is evidence or input.
	// Production runs on loopback behind Caddy, which appends the address it
	// saw; development has nothing in front of it.
	TrustProxy bool
	// Reflection serves the gRPC reflection API, in development only.
	Reflection  bool
	LogRequests bool

	S3 S3

	// EngineToken and OperatorToken are bearer tokens, one per scope.
	// DeviceTokenSecret derives one token per NMI. Reads need none of them.
	EngineToken       string
	OperatorToken     string
	DeviceTokenSecret string

	// APIURL is where the engine and the simulator find the API.
	APIURL string

	// ClockSpeed and ClockAnchor define feeder time: it equals wall-clock
	// time at the anchor and runs ClockSpeed times as fast from there. At
	// speed 1 the anchor does not matter.
	ClockSpeed  float64
	ClockAnchor time.Time

	// MetricsAddr serves Prometheus metrics. It must stay on loopback.
	MetricsAddr string
	// OTLPEndpoint receives traces when set. In development traces go to
	// stdout otherwise; in production they are dropped.
	OTLPEndpoint string

	// AnthropicAPIKey enables the assistant. Empty means the assistant is
	// off.
	AnthropicAPIKey string
}

// Load reads the environment. It returns an error rather than exiting so that
// tests can construct a Config without a process boundary.
func Load() (Config, error) {
	c := Config{
		DatabaseURL:     env("DATABASE_URL", defaultDatabaseURL),
		Host:            env("HOST", "127.0.0.1"),
		Version:         env("DOELAB_VERSION", "dev"),
		APIURL:          env("API_URL", "http://localhost:3100"),
		MetricsAddr:     env("METRICS_ADDR", "127.0.0.1:9464"),
		OTLPEndpoint:    env("OTEL_EXPORTER_OTLP_ENDPOINT", ""),
		AnthropicAPIKey: env("ANTHROPIC_API_KEY", ""),
	}

	var err error
	if c.Env, err = parseEnvironment(env("DOELAB_ENV", string(Production))); err != nil {
		return c, err
	}
	dev := c.Env == Development
	c.LogRequests = dev
	c.Reflection = dev
	c.TrustProxy = !dev

	if c.Port, err = envInt("PORT", defaultPort); err != nil {
		return c, err
	}
	maxConns, err := envInt("DB_MAX_CONNS", 16)
	if err != nil {
		return c, err
	}
	if maxConns < 1 || maxConns > 1000 {
		return c, fmt.Errorf("DB_MAX_CONNS: expected 1 to 1000, got %d", maxConns)
	}
	c.DBMaxConns = int32(maxConns)

	timeoutMS, err := envInt("REQUEST_TIMEOUT_MS", 15_000)
	if err != nil {
		return c, err
	}
	c.RequestTimeout = time.Duration(timeoutMS) * time.Millisecond

	c.S3 = S3{
		Endpoint:        env("S3_ENDPOINT", "http://localhost:9200"),
		Region:          env("S3_REGION", "us-east-1"),
		Bucket:          env("S3_BUCKET", "doelab"),
		AccessKeyID:     env("S3_ACCESS_KEY_ID", devS3AccessKey),
		SecretAccessKey: env("S3_SECRET_ACCESS_KEY", devS3SecretKey),
	}
	if c.S3.PathStyle, err = envBool("S3_PATH_STYLE", dev); err != nil {
		return c, err
	}

	if c.ClockSpeed, err = envFloat("DEMO_CLOCK_SPEED", 1); err != nil {
		return c, err
	}
	// The Unix epoch is as good an anchor as any at speed 1. A faster clock
	// must say where it starts: feeder time would otherwise be decades ahead.
	c.ClockAnchor = time.Unix(0, 0).UTC()
	if raw := env("DEMO_CLOCK_ANCHOR", ""); raw != "" {
		if c.ClockAnchor, err = time.Parse(time.RFC3339, raw); err != nil {
			return c, fmt.Errorf("DEMO_CLOCK_ANCHOR: expected an RFC 3339 time, got %q", raw)
		}
	} else if c.ClockSpeed != 1 {
		return c, errors.New("DEMO_CLOCK_ANCHOR: required when DEMO_CLOCK_SPEED is not 1")
	}

	c.EngineToken = env("ENGINE_TOKEN", devEngineToken)
	c.OperatorToken = env("OPERATOR_TOKEN", devOperatorToken)
	c.DeviceTokenSecret = env("DEVICE_TOKEN_SECRET", devDeviceSecret)

	if !dev {
		if err := c.refuseDevSecrets(); err != nil {
			return c, err
		}
	}
	return c, nil
}

// refuseDevSecrets fails closed: production must not start with a credential
// that is printed in .env.example, or with a metrics listener off loopback.
func (c Config) refuseDevSecrets() error {
	for name, pair := range map[string][2]string{
		"ENGINE_TOKEN":         {c.EngineToken, devEngineToken},
		"OPERATOR_TOKEN":       {c.OperatorToken, devOperatorToken},
		"DEVICE_TOKEN_SECRET":  {c.DeviceTokenSecret, devDeviceSecret},
		"S3_SECRET_ACCESS_KEY": {c.S3.SecretAccessKey, devS3SecretKey},
	} {
		if pair[0] == pair[1] {
			return fmt.Errorf("%s: production refuses the development default; set a real value", name)
		}
		if len(pair[0]) < 24 {
			return fmt.Errorf("%s: production needs at least 24 characters", name)
		}
	}
	if c.EngineToken == c.OperatorToken {
		return errors.New("ENGINE_TOKEN and OPERATOR_TOKEN must differ: one token must not carry two scopes")
	}
	if !strings.HasPrefix(c.MetricsAddr, "127.0.0.1:") && !strings.HasPrefix(c.MetricsAddr, "localhost:") {
		return fmt.Errorf("METRICS_ADDR: production serves metrics on loopback only, got %q", c.MetricsAddr)
	}
	return nil
}

// parseEnvironment refuses what it does not recognise rather than treating it
// as "not production". A typo is the exact case this exists to catch.
//
// "test" is accepted as development: the test suites set it, and they want the
// dev posture.
func parseEnvironment(raw string) (Environment, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "production":
		return Production, nil
	case "development", "test":
		return Development, nil
	default:
		return Production, fmt.Errorf(
			"DOELAB_ENV: expected production, development or test, got %q", raw)
	}
}

func env(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) (int, error) {
	raw := env(key, "")
	if raw == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s: expected an integer, got %q", key, raw)
	}
	return n, nil
}

func envFloat(key string, fallback float64) (float64, error) {
	raw := env(key, "")
	if raw == "" {
		return fallback, nil
	}
	f, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, fmt.Errorf("%s: expected a number, got %q", key, raw)
	}
	return f, nil
}

func envBool(key string, fallback bool) (bool, error) {
	raw := env(key, "")
	if raw == "" {
		return fallback, nil
	}
	b, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("%s: expected true or false, got %q", key, raw)
	}
	return b, nil
}
