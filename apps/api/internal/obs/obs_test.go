package obs

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"go.opentelemetry.io/otel/trace"

	doelabv1 "doelab/api/gen/doelab/v1"
	"doelab/api/gen/doelab/v1/doelabv1connect"
	"doelab/api/internal/domain"
	"doelab/api/internal/service"
)

// The recorder is what the services are given.
var _ service.Recorder = (*Recorder)(nil)

func newTelemetry(t *testing.T, endpoint string) *Telemetry {
	t.Helper()
	tel, err := New(context.Background(), Options{Service: "doelab-api", Version: "test", Env: "test", OTLPEndpoint: endpoint})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		// With no collector to send to, a shutdown may fail to flush.
		_ = tel.Shutdown(ctx)
	})
	return tel
}

// scrape returns the metrics as a scraper sees them.
func scrape(t *testing.T, tel *Telemetry) string {
	t.Helper()
	rec := httptest.NewRecorder()
	tel.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("/metrics answered %d", rec.Code)
	}
	return rec.Body.String()
}

func wantLines(t *testing.T, metrics string, lines ...string) {
	t.Helper()
	for _, line := range lines {
		if !strings.Contains(metrics, line) {
			t.Errorf("the metrics have no line %q", line)
		}
	}
	if t.Failed() {
		for _, line := range strings.Split(metrics, "\n") {
			if strings.HasPrefix(line, "doelab_") || strings.HasPrefix(line, "rpc_") {
				t.Log(line)
			}
		}
	}
}

func TestRecorder(t *testing.T) {
	t.Parallel()
	tel := newTelemetry(t, "")
	r, ctx := tel.Recorder, context.Background()
	wall := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)
	r.now = func() time.Time { return wall }

	r.SubscriptionOpened(ctx)
	r.SubscriptionOpened(ctx)
	r.SubscriptionOpened(ctx)
	r.SubscriptionClosed(ctx)
	r.EnvelopesPublished(ctx, 2688)
	r.EnvelopesPublished(ctx, 12)
	// 40 ms from the write to the send; and one from a clock a hair ahead.
	r.EnvelopeDispatched(ctx, wall.Add(-40*time.Millisecond))
	r.EnvelopeDispatched(ctx, wall.Add(time.Millisecond))
	r.RunCompleted(ctx, domain.RunCompleted, 300*time.Millisecond)
	r.RunCompleted(ctx, domain.RunFailed, 7*time.Second)
	r.AlertOpened(ctx, domain.AlertConstraintBreach, domain.SeverityCritical)
	r.AlertOpened(ctx, domain.AlertDeviceOffline, domain.SeverityInfo)
	r.AlertOpened(ctx, domain.AlertDeviceOffline, domain.SeverityInfo)
	r.ReadingsStored(ctx, 76)

	wantLines(t, scrape(t, tel),
		`doelab_subscriptions_active{`, `} 2`,
		`doelab_envelopes_published_total{`, `} 2700`,
		// One sample at or under 1 ms (the clamped one), two at or under 50 ms.
		`doelab_envelope_dispatch_latency_seconds_bucket{`, `le="0.001"} 1`, `le="0.05"} 2`,
		`doelab_envelope_dispatch_latency_seconds_sum{`, `} 0.04`,
		`doelab_engine_run_duration_seconds_count{`, `status="completed"} 1`, `status="failed"} 1`,
		`doelab_alerts_opened_total{kind="constraint_breach"`, `severity="critical"} 1`,
		`kind="device_offline"`, `severity="info"} 2`,
		`doelab_readings_stored_total{`, `} 76`,
		// The process itself.
		`go_goroutines `, `process_resident_memory_bytes `,
		// Who is speaking.
		`service_name="doelab-api"`,
	)
}

type clockService struct {
	doelabv1connect.UnimplementedClockServiceHandler
	log *slog.Logger
}

func (c clockService) GetClock(ctx context.Context, _ *connect.Request[doelabv1.GetClockRequest]) (*connect.Response[doelabv1.GetClockResponse], error) {
	c.log.InfoContext(ctx, "asked the time")
	return connect.NewResponse(&doelabv1.GetClockResponse{Speed: 60}), nil
}

// An RPC through the interceptor is measured by procedure, and what it logs
// carries its trace id.
func TestInterceptor(t *testing.T) {
	t.Parallel()
	// An endpoint that nothing listens on: building the exporter does not
	// connect, and a request is served whether or not its span is delivered.
	tel := newTelemetry(t, "http://127.0.0.1:9/v1/traces")
	var logged bytes.Buffer
	log := slog.New(LogHandler(slog.NewJSONHandler(&logged, nil)))

	mux := http.NewServeMux()
	mux.Handle(doelabv1connect.NewClockServiceHandler(clockService{log: log}, connect.WithInterceptors(tel.Interceptor)))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := doelabv1connect.NewClockServiceClient(srv.Client(), srv.URL)
	res, err := client.GetClock(context.Background(), connect.NewRequest(&doelabv1.GetClockRequest{}))
	if err != nil || res.Msg.GetSpeed() != 60 {
		t.Fatalf("GetClock = %v, %v", res, err)
	}

	wantLines(t, scrape(t, tel),
		`rpc_server_call_duration_seconds_count{`, `rpc_method="doelab.v1.ClockService/GetClock"`,
		`rpc_response_status_code="OK"`,
	)
	line := logged.String()
	if !strings.Contains(line, `"msg":"asked the time"`) || !strings.Contains(line, `"trace_id":"`) || !strings.Contains(line, `"span_id":"`) {
		t.Errorf("the handler's log line = %s, want a trace id and a span id", line)
	}
}

func TestLogHandler(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	log := slog.New(LogHandler(slog.NewJSONHandler(&out, &slog.HandlerOptions{Level: slog.LevelInfo})))

	// Outside a request there is no trace to name.
	log.InfoContext(context.Background(), "starting")
	if strings.Contains(out.String(), "trace_id") {
		t.Errorf("a line outside a request = %s", out.String())
	}
	out.Reset()

	span := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: trace.TraceID{0xab, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15},
		SpanID:  trace.SpanID{0xcd, 1, 2, 3, 4, 5, 6, 7},
	})
	ctx := trace.ContextWithSpanContext(context.Background(), span)
	// Attributes and groups that were added to the logger stay.
	log.With("feeder", "LV10").WithGroup("run").InfoContext(ctx, "published", "envelopes", 2688)
	line := out.String()
	for _, want := range []string{
		`"trace_id":"ab0102030405060708090a0b0c0d0e0f"`, `"span_id":"cd01020304050607"`,
		`"feeder":"LV10"`, `"envelopes":2688`, `"run":{`,
	} {
		if !strings.Contains(line, want) {
			t.Errorf("the line has no %s: %s", want, line)
		}
	}

	// The level of the wrapped handler still decides what is written.
	out.Reset()
	log.DebugContext(ctx, "too quiet")
	if out.Len() != 0 {
		t.Errorf("a debug line was written: %s", out.String())
	}
}

func TestNewRejectsABadEndpoint(t *testing.T) {
	t.Parallel()
	for _, endpoint := range []string{"://not a url", "collector:4318", "ftp://collector/v1/traces", "http://"} {
		_, err := New(context.Background(), Options{Service: "doelab-api", OTLPEndpoint: endpoint})
		if err == nil || !strings.Contains(err.Error(), "is not an http or https URL") {
			t.Errorf("endpoint %q: error = %v", endpoint, err)
		}
	}
}
