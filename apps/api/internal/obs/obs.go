// Package obs is the observability of the API: a trace and a duration for
// every RPC, counters for what the services do, and the trace id on every
// log line that belongs to a request.
//
// Metrics are served in Prometheus' text format, for a scraper on the same
// host. Traces are exported over OTLP when an endpoint is configured; without
// one a request still has a trace id, for its log lines, and nothing leaves
// the process.
package obs

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"connectrpc.com/connect"
	"connectrpc.com/otelconnect"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	otelprom "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"doelab/api/internal/domain"
)

// Options say who is being observed, and where its traces go.
type Options struct {
	// Service, Version and Env name the process on every metric and span.
	Service string
	Version string
	Env     string
	// OTLPEndpoint is the URL of an OTLP/HTTP collector. Empty exports no
	// traces.
	OTLPEndpoint string
}

// Telemetry is the providers of one process and what is built on them.
type Telemetry struct {
	// Interceptor traces and measures every RPC. It goes outermost.
	Interceptor connect.Interceptor
	// Recorder counts what the services do.
	Recorder *Recorder
	// Handler serves the metrics in Prometheus' text format.
	Handler http.Handler

	meters *sdkmetric.MeterProvider
	traces *sdktrace.TracerProvider
}

// New builds the providers. Nothing is registered globally: what is observed
// is what is handed this Telemetry.
func New(ctx context.Context, o Options) (*Telemetry, error) {
	res, err := resource.New(ctx, resource.WithAttributes(
		attribute.String("service.name", o.Service),
		attribute.String("service.version", o.Version),
		attribute.String("deployment.environment.name", o.Env),
	))
	if err != nil {
		return nil, fmt.Errorf("resource: %w", err)
	}

	// A registry of its own, so that a test can build two.
	registry := prometheus.NewRegistry()
	registry.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	exporter, err := otelprom.New(otelprom.WithRegisterer(registry))
	if err != nil {
		return nil, fmt.Errorf("prometheus exporter: %w", err)
	}
	meters := sdkmetric.NewMeterProvider(sdkmetric.WithResource(res), sdkmetric.WithReader(exporter))

	// Every request is sampled: at this volume the trace id on a log line is
	// worth more than the spans it costs.
	traceOptions := []sdktrace.TracerProviderOption{sdktrace.WithResource(res), sdktrace.WithSampler(sdktrace.AlwaysSample())}
	if o.OTLPEndpoint != "" {
		// The exporter takes any string and fails later, at the first export,
		// in a log line. A wrong endpoint is a mistake to hear about now.
		if u, err := url.Parse(o.OTLPEndpoint); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return nil, fmt.Errorf("OTLP endpoint %q is not an http or https URL", o.OTLPEndpoint)
		}
		spans, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(o.OTLPEndpoint))
		if err != nil {
			return nil, fmt.Errorf("OTLP exporter for %s: %w", o.OTLPEndpoint, err)
		}
		traceOptions = append(traceOptions, sdktrace.WithBatcher(spans))
	}
	traces := sdktrace.NewTracerProvider(traceOptions...)

	interceptor, err := otelconnect.NewInterceptor(
		otelconnect.WithMeterProvider(meters),
		otelconnect.WithTracerProvider(traces),
		otelconnect.WithPropagator(propagation.TraceContext{}),
	)
	if err != nil {
		return nil, fmt.Errorf("RPC interceptor: %w", err)
	}
	recorder, err := newRecorder(meters.Meter("doelab"))
	if err != nil {
		return nil, fmt.Errorf("instruments: %w", err)
	}
	return &Telemetry{
		Interceptor: interceptor, Recorder: recorder,
		Handler: promhttp.HandlerFor(registry, promhttp.HandlerOpts{}),
		meters:  meters, traces: traces,
	}, nil
}

// Shutdown sends what is buffered and stops the providers.
func (t *Telemetry) Shutdown(ctx context.Context) error {
	return errors.Join(t.traces.Shutdown(ctx), t.meters.Shutdown(ctx))
}

// Recorder counts what the services do. It implements service.Recorder.
type Recorder struct {
	subscriptions metric.Int64UpDownCounter
	published     metric.Int64Counter
	dispatch      metric.Float64Histogram
	runs          metric.Float64Histogram
	alerts        metric.Int64Counter
	readings      metric.Int64Counter
	// now is the wall clock, replaceable in tests.
	now func() time.Time
}

// Bucket bounds, in seconds. A dispatch is a database read and a stream
// write; a run of the engine is a few hundred power flows.
var (
	dispatchBuckets = []float64{0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30}
	runBuckets      = []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60}
)

func newRecorder(m metric.Meter) (*Recorder, error) {
	r := &Recorder{now: time.Now}
	var errs [6]error
	r.subscriptions, errs[0] = m.Int64UpDownCounter("doelab.subscriptions.active",
		metric.WithDescription("Envelope subscriptions that are open now."))
	r.published, errs[1] = m.Int64Counter("doelab.envelopes.published",
		metric.WithDescription("Envelopes written by the engine's publishes."))
	r.dispatch, errs[2] = m.Float64Histogram("doelab.envelope.dispatch.latency",
		metric.WithDescription("From the write of an envelope to its send on a subscription."),
		metric.WithUnit("s"), metric.WithExplicitBucketBoundaries(dispatchBuckets...))
	r.runs, errs[3] = m.Float64Histogram("doelab.engine.run.duration",
		metric.WithDescription("How long a run of the engine took, by how it ended."),
		metric.WithUnit("s"), metric.WithExplicitBucketBoundaries(runBuckets...))
	r.alerts, errs[4] = m.Int64Counter("doelab.alerts.opened",
		metric.WithDescription("Alerts opened, by kind and severity."))
	r.readings, errs[5] = m.Int64Counter("doelab.readings.stored",
		metric.WithDescription("Readings stored from the devices."))
	return r, errors.Join(errs[:]...)
}

// SubscriptionOpened counts a subscription in.
func (r *Recorder) SubscriptionOpened(ctx context.Context) { r.subscriptions.Add(ctx, 1) }

// SubscriptionClosed counts a subscription out.
func (r *Recorder) SubscriptionClosed(ctx context.Context) { r.subscriptions.Add(ctx, -1) }

// EnvelopesPublished counts envelopes that a publish wrote.
func (r *Recorder) EnvelopesPublished(ctx context.Context, n int) {
	r.published.Add(ctx, int64(n))
}

// EnvelopeDispatched records how long an envelope took from its write, at
// created on the wall clock, to its send, which is now.
func (r *Recorder) EnvelopeDispatched(ctx context.Context, created time.Time) {
	// Never negative: the database's clock and this one may differ by a hair.
	r.dispatch.Record(ctx, max(r.now().Sub(created).Seconds(), 0))
}

// RunCompleted records how a run of the engine ended, and how long it took.
func (r *Recorder) RunCompleted(ctx context.Context, status domain.RunStatus, took time.Duration) {
	r.runs.Record(ctx, took.Seconds(), metric.WithAttributes(attribute.String("status", string(status))))
}

// AlertOpened counts a new alert.
func (r *Recorder) AlertOpened(ctx context.Context, kind domain.AlertKind, severity domain.AlertSeverity) {
	r.alerts.Add(ctx, 1, metric.WithAttributes(
		attribute.String("kind", string(kind)), attribute.String("severity", string(severity))))
}

// ReadingsStored counts readings that an ingest stored.
func (r *Recorder) ReadingsStored(ctx context.Context, n int) {
	r.readings.Add(ctx, int64(n))
}
