package server

import (
	"doelab/api/gen/doelab/v1/doelabv1connect"
	"doelab/api/internal/auth"
	"doelab/api/internal/interceptor"
)

// Policy says which scope each procedure needs. Reads are public; a write
// names the one scope that may make it.
//
// Every procedure the server mounts must be listed: the auth interceptor
// refuses one that is not, and policy_test.go fails when a procedure of the
// proto package is missing here.
func Policy() interceptor.Policy {
	const (
		public   = auth.ScopePublic
		operator = auth.ScopeOperator
		engine   = auth.ScopeEngine
		device   = auth.ScopeDevice
	)
	return interceptor.Policy{
		doelabv1connect.FeederServiceGetFeederProcedure:        public,
		doelabv1connect.FeederServiceListFeedersProcedure:      public,
		doelabv1connect.FeederServiceUpdateFeederProcedure:     operator,
		doelabv1connect.FeederServiceGetFeederNodeProcedure:    public,
		doelabv1connect.FeederServiceListFeederNodesProcedure:  public,
		doelabv1connect.FeederServiceGetFeederLineProcedure:    public,
		doelabv1connect.FeederServiceListFeederLinesProcedure:  public,
		doelabv1connect.FeederServiceUpdateFeederLineProcedure: operator,

		doelabv1connect.SiteServiceGetSiteProcedure:          public,
		doelabv1connect.SiteServiceListSitesProcedure:        public,
		doelabv1connect.SiteServiceCreateSiteProcedure:       operator,
		doelabv1connect.SiteServiceUpdateSiteProcedure:       operator,
		doelabv1connect.SiteServiceDeleteSiteProcedure:       operator,
		doelabv1connect.SiteServiceListSiteProfilesProcedure: public,

		doelabv1connect.DeviceServiceGetDeviceProcedure:    public,
		doelabv1connect.DeviceServiceListDevicesProcedure:  public,
		doelabv1connect.DeviceServiceCreateDeviceProcedure: operator,
		doelabv1connect.DeviceServiceUpdateDeviceProcedure: operator,
		doelabv1connect.DeviceServiceDeleteDeviceProcedure: operator,

		doelabv1connect.EnvelopeConfigServiceGetEnvelopeConfigProcedure:       public,
		doelabv1connect.EnvelopeConfigServiceGetActiveEnvelopeConfigProcedure: public,
		doelabv1connect.EnvelopeConfigServiceListEnvelopeConfigsProcedure:     public,
		doelabv1connect.EnvelopeConfigServiceCreateEnvelopeConfigProcedure:    operator,

		doelabv1connect.FeederServiceGetFeederForecastProcedure: public,

		doelabv1connect.EnvelopeRunServiceGetEnvelopeRunProcedure:      public,
		doelabv1connect.EnvelopeRunServiceListEnvelopeRunsProcedure:    public,
		doelabv1connect.EnvelopeRunServiceCreateEnvelopeRunProcedure:   engine,
		doelabv1connect.EnvelopeRunServiceCompleteEnvelopeRunProcedure: engine,

		doelabv1connect.EnvelopeServiceGetCurrentEnvelopeProcedure: public,
		doelabv1connect.EnvelopeServiceListEnvelopesProcedure:      public,
		doelabv1connect.EnvelopeServicePublishEnvelopesProcedure:   engine,
		// The scope gets a device in; the service then checks that the token
		// is for the NMI it asks about.
		doelabv1connect.EnvelopeServiceSubscribeEnvelopesProcedure: device,

		doelabv1connect.ClockServiceGetClockProcedure: public,

		doelabv1connect.EnvelopeRunServiceCreateEnvelopeRunIntervalsProcedure: engine,
		doelabv1connect.EnvelopeRunServiceListEnvelopeRunIntervalsProcedure:   public,

		doelabv1connect.TelemetryServiceListReadingsProcedure:    public,
		doelabv1connect.TelemetryServiceIngestReadingsProcedure:  device,
		doelabv1connect.TelemetryServiceGetFleetSummaryProcedure: public,
		doelabv1connect.TelemetryServiceWatchFleetProcedure:      public,
		doelabv1connect.TelemetryServiceGetFeederSeriesProcedure: public,
		doelabv1connect.TelemetryServiceGetSiteSeriesProcedure:   public,
		doelabv1connect.TelemetryServiceGetDailyReportProcedure:  public,

		doelabv1connect.AlertServiceGetAlertProcedure:         public,
		doelabv1connect.AlertServiceListAlertsProcedure:       public,
		doelabv1connect.AlertServiceAcknowledgeAlertProcedure: operator,

		doelabv1connect.BackstopServiceGetBackstopEventProcedure:    public,
		doelabv1connect.BackstopServiceListBackstopEventsProcedure:  public,
		doelabv1connect.BackstopServiceCreateBackstopEventProcedure: operator,
		doelabv1connect.BackstopServiceClearBackstopProcedure:       operator,
	}
}
