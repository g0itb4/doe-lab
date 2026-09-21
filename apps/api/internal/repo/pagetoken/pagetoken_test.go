package pagetoken

import (
	"errors"
	"reflect"
	"testing"

	"doelab/api/internal/domain"
)

func TestRoundTrip(t *testing.T) {
	t.Parallel()

	token := Encode("2026-10-01T00:00:00Z", "0199-abc")
	parts, err := Decode(token, 2)
	if err != nil || !reflect.DeepEqual(parts, []string{"2026-10-01T00:00:00Z", "0199-abc"}) {
		t.Errorf("Decode = %v, %v", parts, err)
	}

	first, err := Decode("", 2)
	if err != nil || !reflect.DeepEqual(first, []string{"", ""}) {
		t.Errorf("first page = %v, %v", first, err)
	}
}

func TestDecodeRejectsForeignTokens(t *testing.T) {
	t.Parallel()

	for name, token := range map[string]string{
		"not base64":       "!!!",
		"not json":         "bm90IGpzb24",
		"wrong part count": Encode("only-one"),
		"json object":      "e30",
	} {
		if _, err := Decode(token, 2); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("%s: error = %v, want ErrInvalid", name, err)
		}
	}
}

func TestNext(t *testing.T) {
	t.Parallel()
	key := func(s string) []string { return []string{s} }

	rows, token := Next([]string{"a", "b", "c"}, 2, key)
	if !reflect.DeepEqual(rows, []string{"a", "b"}) || token != Encode("b") {
		t.Errorf("more rows than the page: %v, %q", rows, token)
	}
	rows, token = Next([]string{"a", "b"}, 2, key)
	if len(rows) != 2 || token != "" {
		t.Errorf("exactly a page: %v, %q", rows, token)
	}
	rows, token = Next([]string{}, 2, key)
	if len(rows) != 0 || token != "" {
		t.Errorf("no rows: %v, %q", rows, token)
	}
}
