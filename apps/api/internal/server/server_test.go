package server_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"

	doelabv1 "doelab/api/gen/doelab/v1"
	"doelab/api/gen/doelab/v1/doelabv1connect"
	"doelab/api/internal/config"
	"doelab/api/internal/server"
)

// Every procedure of the proto package has an explicit scope. A new RPC that
// nobody decided about is not served to everyone: it is not served at all, and
// this test says so first.
func TestPolicyCoversEveryProcedure(t *testing.T) {
	t.Parallel()
	policy := server.Policy()

	declared := map[string]bool{}
	protoregistry.GlobalFiles.RangeFilesByPackage("doelab.v1", func(fd protoreflect.FileDescriptor) bool {
		for i := range fd.Services().Len() {
			svc := fd.Services().Get(i)
			for j := range svc.Methods().Len() {
				declared["/"+string(svc.FullName())+"/"+string(svc.Methods().Get(j).Name())] = true
			}
		}
		return true
	})
	if len(declared) == 0 {
		t.Fatal("no procedures found in doelab.v1")
	}
	for procedure := range declared {
		if _, ok := policy[procedure]; !ok {
			t.Errorf("%s has no scope in server.Policy()", procedure)
		}
	}
	for procedure := range policy {
		if !declared[procedure] {
			t.Errorf("server.Policy() names %s, which is not a procedure of doelab.v1", procedure)
		}
	}
}

func (a *api) post(t *testing.T, path, body string, header ...string) (int, string) {
	t.Helper()
	r, err := http.NewRequest(http.MethodPost, a.URL+path, strings.NewReader(body))
	noErr(t, "request", err)
	r.Header.Set("Content-Type", "application/json")
	for i := 0; i+1 < len(header); i += 2 {
		r.Header.Set(header[i], header[i+1])
	}
	res, err := a.HTTP.Do(r)
	noErr(t, "post", err)
	defer func() { _ = res.Body.Close() }()
	out, err := io.ReadAll(res.Body)
	noErr(t, "read", err)
	return res.StatusCode, string(out)
}

// The health check is the gRPC Health service over plain HTTP and JSON, which
// is what the deploy script calls, and it reports the database.
func TestHealth(t *testing.T) {
	t.Parallel()
	a := newAPI(t)

	status, body := a.post(t, "/grpc.health.v1.Health/Check", `{"service":""}`)
	if status != http.StatusOK || !strings.Contains(body, `"SERVING_STATUS_SERVING"`) {
		t.Errorf("health = %d %s", status, body)
	}
	// Under /rpc too, where the web app's proxy reaches it.
	status, body = a.post(t, "/rpc/grpc.health.v1.Health/Check", `{"service":"doelab.v1.SiteService"}`)
	if status != http.StatusOK || !strings.Contains(body, `"SERVING_STATUS_SERVING"`) {
		t.Errorf("health under /rpc = %d %s", status, body)
	}
	status, body = a.post(t, "/grpc.health.v1.Health/Check", `{"service":"no.such.Service"}`)
	if status != http.StatusNotFound {
		t.Errorf("health of an unknown service = %d %s", status, body)
	}

	a.DB.Err = errors.New("connection refused")
	status, body = a.post(t, "/grpc.health.v1.Health/Check", `{"service":""}`)
	if status != http.StatusOK || !strings.Contains(body, `"SERVING_STATUS_NOT_SERVING"`) {
		t.Errorf("health with the database down = %d %s", status, body)
	}
}

// Both mounts serve the same handler: the root for gRPC stubs, /rpc for the
// web app.
func TestBothMounts(t *testing.T) {
	t.Parallel()
	a := newAPI(t)
	for _, prefix := range []string{"", "/rpc"} {
		status, body := a.post(t, prefix+"/doelab.v1.FeederService/ListFeeders", `{}`)
		if status != http.StatusOK || !strings.Contains(body, `"LV10"`) {
			t.Errorf("%s/doelab.v1.FeederService/ListFeeders = %d %s", prefix, status, body)
		}
	}
	// Field names on the wire are the schema's, in the JSON form too.
	status, body := a.post(t, "/doelab.v1.SiteService/ListSites", `{"feederId":"`+a.fixture.Feeder.ID.String()+`"}`)
	if status != http.StatusOK || !strings.Contains(body, `"exportCapW":5000`) || !strings.Contains(body, `"nmi":"`+a.fixture.SiteA.NMI+`"`) {
		t.Errorf("ListSites = %d %s", status, body)
	}
}

func TestReflectionInDevelopmentOnly(t *testing.T) {
	t.Parallel()
	a := newAPI(t) // development: reflection on
	status, _ := a.post(t, "/grpc.reflection.v1.ServerReflection/ServerReflectionInfo", `{}`)
	if status == http.StatusNotFound {
		t.Error("reflection is not mounted in development")
	}

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	prod := server.Handler(config.Config{Env: config.Production, RateLimitPerSecond: 100, RateLimitBurst: 400}, log, a.Deps)
	r, _ := http.NewRequest(http.MethodPost, "/grpc.reflection.v1.ServerReflection/ServerReflectionInfo", strings.NewReader("{}"))
	w := &recorder{header: http.Header{}}
	prod.ServeHTTP(w, r)
	if w.status != http.StatusNotFound {
		t.Errorf("reflection in production = %d, want 404", w.status)
	}
}

type recorder struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (r *recorder) Header() http.Header         { return r.header }
func (r *recorder) WriteHeader(status int)      { r.status = status }
func (r *recorder) Write(b []byte) (int, error) { return r.body.Write(b) }

func TestAuthOnPublicProcedures(t *testing.T) {
	t.Parallel()
	a := newAPI(t)
	list := func(token string) error {
		_, err := a.feeders(token).ListFeeders(ctx, req(&doelabv1.ListFeedersRequest{}))
		return err
	}
	noErr(t, "anonymous", list(""))
	noErr(t, "operator", list(operatorToken))
	noErr(t, "engine", list(engineToken))
	noErr(t, "device", list(a.Tokens.DeviceToken(a.fixture.SiteA.NMI)))
	// A token that does not verify is refused even where none is needed, so
	// a misconfigured client finds out on its first call.
	wantCode(t, "a wrong token", list("not-a-token"), connect.CodeUnauthenticated)

	// The reason is not echoed.
	_, err := a.sites("wrong").DeleteSite(ctx, req(&doelabv1.DeleteSiteRequest{Id: a.fixture.SiteA.ID.String()}))
	var ce *connect.Error
	if !errors.As(err, &ce) || ce.Message() != "valid credentials are required" {
		t.Errorf("error = %v", err)
	}
}

func TestNewServer(t *testing.T) {
	t.Parallel()
	a := newAPI(t)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := server.New(config.Config{Host: "127.0.0.1", Port: 3100, Env: config.Production, RateLimitPerSecond: 100, RateLimitBurst: 400}, log, a.Deps)
	if srv.Addr != "127.0.0.1:3100" || srv.Handler == nil || srv.ReadHeaderTimeout == 0 {
		t.Errorf("server = %+v", srv)
	}
	// A stream must not be capped at a fixed wall clock.
	if srv.WriteTimeout != 0 || srv.ReadTimeout != 0 {
		t.Errorf("WriteTimeout %v and ReadTimeout %v would cut streams", srv.WriteTimeout, srv.ReadTimeout)
	}
}

// panicking is a feeder service whose every method panics.
type panicking struct {
	doelabv1connect.FeederServiceHandler
}

func (panicking) ListFeeders(context.Context, *connect.Request[doelabv1.ListFeedersRequest]) (*connect.Response[doelabv1.ListFeedersResponse], error) {
	panic("a bug with password=hunter2 in it")
}

// A panic in a handler is logged with its stack and answered as an internal
// error that says nothing.
func TestPanicIsRecovered(t *testing.T) {
	t.Parallel()
	a := newAPI(t)
	var out bytes.Buffer
	deps := a.Deps
	deps.Feeders = panicking{}
	handler := server.Handler(config.Config{Env: config.Production, RateLimitPerSecond: 100, RateLimitBurst: 400}, slog.New(slog.NewTextHandler(&out, nil)), deps)

	r, _ := http.NewRequest(http.MethodPost, "/doelab.v1.FeederService/ListFeeders", strings.NewReader("{}"))
	r.Header.Set("Content-Type", "application/json")
	w := &recorder{header: http.Header{}}
	handler.ServeHTTP(w, r)

	if w.status != http.StatusInternalServerError || !strings.Contains(w.body.String(), "internal error") || strings.Contains(w.body.String(), "hunter2") {
		t.Errorf("response = %d %s", w.status, w.body.String())
	}
	if !strings.Contains(out.String(), "hunter2") || !strings.Contains(out.String(), "ListFeeders") || !strings.Contains(out.String(), "stack=") {
		t.Errorf("log = %q", out.String())
	}
}
