package domain

import (
	"fmt"
	"regexp"
)

// SyntheticNMIPrefix starts every NMI this system makes up.
//
// AEMO's NMI Allocation List (version 14, November 2025) allocates
// alphanumeric blocks under the jurisdiction letters N, Q, S, T, V and W, and
// "A" for transmission; no block starts with X. So an NMI that starts
// "XDLAB" belongs to no network and no customer, and reads as synthetic at a
// glance. It also keeps to the list's own rules: no letter O or I, and no W
// as the fifth character.
const SyntheticNMIPrefix = "XDLAB"

// maxSyntheticSerial is the largest serial that fits the five digits after
// the prefix.
const maxSyntheticSerial = 99999

var nmiFormat = regexp.MustCompile(`^[A-Z0-9]{10}[0-9]$`)

// NMIChecksum returns the checksum digit of a 10-character NMI, by the
// algorithm of AEMO's NMI Procedure: from the right, double the ASCII code of
// every other character, starting with the rightmost; add the decimal digits
// of every value; the checksum brings the total to a multiple of ten.
//
// The schema holds the same rule as the SQL function nmi_checksum.
func NMIChecksum(nmi10 string) int {
	total := 0
	double := true
	for i := len(nmi10) - 1; i >= 0; i-- {
		v := int(nmi10[i])
		if double {
			v *= 2
		}
		double = !double
		for ; v > 0; v /= 10 {
			total += v % 10
		}
	}
	return (10 - total%10) % 10
}

// ValidNMI reports whether nmi is 10 upper-case letters or digits followed by
// the right checksum digit.
func ValidNMI(nmi string) bool {
	return nmiFormat.MatchString(nmi) && NMIChecksum(nmi[:10]) == int(nmi[10]-'0')
}

// SyntheticNMI returns the 11-character NMI, checksum included, with the
// given serial in the synthetic block.
func SyntheticNMI(serial int) (string, error) {
	if serial < 0 || serial > maxSyntheticSerial {
		return "", fmt.Errorf("%w: NMI serial %d is outside 0 to %d", ErrInvalid, serial, maxSyntheticSerial)
	}
	nmi10 := fmt.Sprintf("%s%05d", SyntheticNMIPrefix, serial)
	return fmt.Sprintf("%s%d", nmi10, NMIChecksum(nmi10)), nil
}
