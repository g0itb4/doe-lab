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
	}
}
