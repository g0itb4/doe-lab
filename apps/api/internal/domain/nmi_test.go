package domain

import (
	"errors"
	"testing"
)

// The worked examples of AEMO's NMI Procedure ("NMI checksum" appendix).
var aemoChecksums = map[string]int{
	"2001985732": 8, "2001985733": 6, "3075621875": 8, "3075621876": 6,
	"4316854005": 9, "4316854006": 7, "6305888444": 6, "6350888444": 2,
	"7001888333": 8, "7102000001": 7, "NAAAMYS582": 6, "NBBBX11110": 0,
	"NBBBX11111": 8, "NCCC519495": 5, "NGGG000055": 4, "QAAAVZZZZZ": 3,
	"QCDWW00010": 2, "SMVEW00085": 8, "VAAA000065": 7, "VAAA000066": 5,
	"VAAA000067": 2, "VAAASTY576": 8, "VCCCX00009": 1, "VEEEX00009": 1,
	"VKTS786150": 2, "VKTS867150": 5, "VKTS871650": 7, "VKTS876105": 7,
	"VKTS876150": 3, "VKTS876510": 8, "1234C6789A": 3,
}

func TestNMIChecksum(t *testing.T) {
	t.Parallel()

	for nmi, want := range aemoChecksums {
		if got := NMIChecksum(nmi); got != want {
			t.Errorf("NMIChecksum(%q) = %d, want %d", nmi, got, want)
		}
	}
}

func TestValidNMI(t *testing.T) {
	t.Parallel()

	valid := []string{"1234C6789A3", "NBBBX111100", "VKTS8761503"}
	for _, nmi := range valid {
		if !ValidNMI(nmi) {
			t.Errorf("ValidNMI(%q) = false, want true", nmi)
		}
	}
	invalid := map[string]string{
		"wrong checksum":      "1234C6789A4",
		"no checksum":         "1234C6789A",
		"too long":            "1234C6789A33",
		"lower case":          "1234c6789a3",
		"letter as checksum":  "1234C6789AA",
		"punctuation":         "1234-6789A3",
		"empty":               "",
		"transposed, same ck": "1243C6789A3",
	}
	for name, nmi := range invalid {
		if ValidNMI(nmi) {
			t.Errorf("%s: ValidNMI(%q) = true, want false", name, nmi)
		}
	}
}

func TestSyntheticNMI(t *testing.T) {
	t.Parallel()

	seen := map[string]bool{}
	for _, serial := range []int{0, 1, 93, 99999} {
		nmi, err := SyntheticNMI(serial)
		if err != nil {
			t.Fatal(err)
		}
		if len(nmi) != 11 || nmi[:5] != SyntheticNMIPrefix || !ValidNMI(nmi) || seen[nmi] {
			t.Errorf("SyntheticNMI(%d) = %q", serial, nmi)
		}
		seen[nmi] = true
	}
	if nmi, _ := SyntheticNMI(1); nmi != "XDLAB00001"+string(rune('0'+NMIChecksum("XDLAB00001"))) {
		t.Errorf("SyntheticNMI(1) = %q", nmi)
	}

	for _, serial := range []int{-1, 100000} {
		if _, err := SyntheticNMI(serial); !errors.Is(err, ErrInvalid) {
			t.Errorf("SyntheticNMI(%d): error = %v, want ErrInvalid", serial, err)
		}
	}
}
