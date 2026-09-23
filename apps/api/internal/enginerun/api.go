// Package enginerun is the engine as a client of the API: it reads the feeder
// and the forecast over RPC, computes the envelopes of a horizon, publishes
// them, and records the run.
//
// The engine never opens the database. That is the boundary the demo is
// about: the network modelling underneath, the product's services on top,
// and one API between them.
package enginerun

import (
	"context"
	"net/http"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	doelabv1 "doelab/api/gen/doelab/v1"
	"doelab/api/gen/doelab/v1/doelabv1connect"
	"doelab/api/internal/domain"
	"doelab/api/internal/protomap"
)

// API is the part of the API that the engine uses.
type API struct {
	Feeders   doelabv1connect.FeederServiceClient
	Sites     doelabv1connect.SiteServiceClient
	Configs   doelabv1connect.EnvelopeConfigServiceClient
	Runs      doelabv1connect.EnvelopeRunServiceClient
	Envelopes doelabv1connect.EnvelopeServiceClient
	Clock     doelabv1connect.ClockServiceClient
}

// NewAPI builds the clients. opts carry the engine's token.
func NewAPI(client connect.HTTPClient, baseURL string, opts ...connect.ClientOption) *API {
	return &API{
		Feeders:   doelabv1connect.NewFeederServiceClient(client, baseURL, opts...),
		Sites:     doelabv1connect.NewSiteServiceClient(client, baseURL, opts...),
		Configs:   doelabv1connect.NewEnvelopeConfigServiceClient(client, baseURL, opts...),
		Runs:      doelabv1connect.NewEnvelopeRunServiceClient(client, baseURL, opts...),
		Envelopes: doelabv1connect.NewEnvelopeServiceClient(client, baseURL, opts...),
		Clock:     doelabv1connect.NewClockServiceClient(client, baseURL, opts...),
	}
}

// Token returns a client option that sends a bearer token on every call.
func Token(token string) connect.ClientOption {
	return connect.WithInterceptors(connect.UnaryInterceptorFunc(func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			req.Header().Set("Authorization", "Bearer "+token)
			return next(ctx, req)
		}
	}))
}

var _ connect.HTTPClient = (*http.Client)(nil)

// pageSize is how many rows a list call asks for.
const pageSize = 500

// feederModel is everything the engine reads about a feeder.
type feederModel struct {
	feeder domain.Feeder
	nodes  []domain.FeederNode
	lines  []domain.FeederLine
	sites  []domain.Site
	config domain.EnvelopeConfig
}

// load reads the feeder, its tree, its sites and its active config.
func (a *API) load(ctx context.Context, code string) (feederModel, error) {
	var m feederModel
	feeder, err := a.Feeders.GetFeeder(ctx, connect.NewRequest(&doelabv1.GetFeederRequest{
		Key: &doelabv1.GetFeederRequest_Code{Code: code},
	}))
	if err != nil {
		return m, err
	}
	m.feeder = protomap.FeederFromMessage(feeder.Msg.GetFeeder())
	id := m.feeder.ID.String()

	for token := ""; ; {
		res, err := a.Feeders.ListFeederNodes(ctx, connect.NewRequest(&doelabv1.ListFeederNodesRequest{
			FeederId: id, PageSize: pageSize, PageToken: token,
		}))
		if err != nil {
			return m, err
		}
		m.nodes = append(m.nodes, protomap.Slice(res.Msg.GetFeederNodes(), protomap.FeederNodeFromMessage)...)
		if token = res.Msg.GetNextPageToken(); token == "" {
			break
		}
	}
	for token := ""; ; {
		res, err := a.Feeders.ListFeederLines(ctx, connect.NewRequest(&doelabv1.ListFeederLinesRequest{
			FeederId: id, PageSize: pageSize, PageToken: token,
		}))
		if err != nil {
			return m, err
		}
		m.lines = append(m.lines, protomap.Slice(res.Msg.GetFeederLines(), protomap.FeederLineFromMessage)...)
		if token = res.Msg.GetNextPageToken(); token == "" {
			break
		}
	}
	for token := ""; ; {
		res, err := a.Sites.ListSites(ctx, connect.NewRequest(&doelabv1.ListSitesRequest{
			FeederId: id, PageSize: pageSize, PageToken: token,
		}))
		if err != nil {
			return m, err
		}
		m.sites = append(m.sites, protomap.Slice(res.Msg.GetSites(), protomap.SiteFromMessage)...)
		if token = res.Msg.GetNextPageToken(); token == "" {
			break
		}
	}

	config, err := a.Configs.GetActiveEnvelopeConfig(ctx, connect.NewRequest(&doelabv1.GetActiveEnvelopeConfigRequest{FeederId: id}))
	if err != nil {
		return m, err
	}
	m.config = protomap.EnvelopeConfigFromMessage(config.Msg.GetEnvelopeConfig())
	return m, nil
}

// forecastKey names one site at one half hour.
type forecastKey struct {
	site uuid.UUID
	ts   int64
}

// forecast reads the load and PV of every site for [from, to).
func (a *API) forecast(ctx context.Context, feederID uuid.UUID, from, to time.Time) (map[forecastKey]*doelabv1.ForecastPoint, error) {
	res, err := a.Feeders.GetFeederForecast(ctx, connect.NewRequest(&doelabv1.GetFeederForecastRequest{
		FeederId: feederID.String(), From: timestamppb.New(from), To: timestamppb.New(to),
	}))
	if err != nil {
		return nil, err
	}
	points := make(map[forecastKey]*doelabv1.ForecastPoint, len(res.Msg.GetPoints()))
	for _, p := range res.Msg.GetPoints() {
		// An id that does not parse becomes the nil UUID, which no site has.
		id, _ := uuid.Parse(p.GetSiteId())
		points[forecastKey{id, p.GetTs().AsTime().Unix()}] = p
	}
	return points, nil
}

// ClockSettings asks the API how feeder time runs: the instant at which it
// equals wall-clock time, and its speed from there. A caller builds its own
// clock from them, so every process keeps the same feeder time.
func (a *API) ClockSettings(ctx context.Context) (anchor time.Time, speed float64, err error) {
	res, err := a.Clock.GetClock(ctx, connect.NewRequest(&doelabv1.GetClockRequest{}))
	if err != nil {
		return time.Time{}, 0, err
	}
	return res.Msg.GetAnchor().AsTime(), res.Msg.GetSpeed(), nil
}
