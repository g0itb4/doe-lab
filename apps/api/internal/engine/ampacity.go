package engine

// assumedAmpacityA is the continuous current rating, in amperes per
// conductor, assumed for each cable type of the CSIRO feeders.
//
// These are assumptions. The dataset's linecodes carry impedances but no
// ratings, so each value below is a typical catalogue rating for that
// conductor size and material, XLPE insulated, laid underground in a duct,
// rounded down. A network operator would replace them with the ratings of its
// own cable schedule; the API lets an operator do that per line.
var assumedAmpacityA = map[string]float64{
	// 16 mm² copper service cable. Typical rating 90 to 100 A; service
	// fuses on such cables are commonly 80 or 100 A.
	"ugsc_16cu_xlpe/nyl/pvc_ug_4w_bundled": 90,
	// 240 mm² aluminium low-voltage distributor, four-core. Typical rating
	// 320 to 390 A depending on the installation; the in-duct end is used.
	"uglv_240al_xlpe/nyl/pvc_ug_4w_bundled": 320,
	// 400 mm² aluminium triplex. Typical rating 480 to 560 A.
	"ughv_400al_triplex_ug_4w_bundled": 480,
}

// AssumedAmpacity returns the assumed rating of a cable type, by OpenDSS
// linecode name. ok is false for a type with no assumed rating; such a line is
// not checked against a thermal limit.
func AssumedAmpacity(linecode string) (amperes float64, ok bool) {
	amperes, ok = assumedAmpacityA[linecode]
	return amperes, ok
}
