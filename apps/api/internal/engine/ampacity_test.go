package engine

import "testing"

func TestAssumedAmpacity(t *testing.T) {
	t.Parallel()

	// Every cable type that LV10 uses has a rating; a bigger conductor
	// carries more.
	service, ok1 := AssumedAmpacity("ugsc_16cu_xlpe/nyl/pvc_ug_4w_bundled")
	main, ok2 := AssumedAmpacity("uglv_240al_xlpe/nyl/pvc_ug_4w_bundled")
	trunk, ok3 := AssumedAmpacity("ughv_400al_triplex_ug_4w_bundled")
	if !ok1 || !ok2 || !ok3 || service >= main || main >= trunk {
		t.Errorf("ratings %v, %v, %v", service, main, trunk)
	}
	if a, ok := AssumedAmpacity("unknown"); ok || a != 0 {
		t.Errorf("unknown linecode = %v, %v", a, ok)
	}

	rated := 0
	for _, b := range lv10().Buses[1:] {
		if b.Line.AmpacityA > 0 {
			rated++
		} else if !b.Line.Switch {
			t.Errorf("line %s (%s) has no rating", b.Line.Name, b.Line.Linecode)
		}
	}
	if rated != 212 {
		t.Errorf("%d rated lines in LV10, want 212 (every line that is not a switch)", rated)
	}
}
