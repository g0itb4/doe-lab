// Package pagetoken encodes the position of a keyset page as an opaque
// string.
//
// A token holds the sort key of the last row of a page. The next page is "the
// rows after that key", which an index answers directly and which stays
// correct when rows are inserted between requests; an offset does neither.
package pagetoken

import (
	"encoding/base64"
	"encoding/json"
	"fmt"

	"doelab/api/internal/domain"
)

// Encode returns the token for a position made of the given key parts.
func Encode(parts ...string) string {
	// A slice of strings always marshals.
	raw, _ := json.Marshal(parts)
	return base64.RawURLEncoding.EncodeToString(raw)
}

// Decode returns the key parts of a token. An empty token is the first page:
// it decodes to n empty parts. A token that was not made by Encode with n
// parts is domain.ErrInvalid.
func Decode(token string, n int) ([]string, error) {
	if token == "" {
		return make([]string, n), nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return nil, fmt.Errorf("%w: malformed page token", domain.ErrInvalid)
	}
	var parts []string
	if err := json.Unmarshal(raw, &parts); err != nil || len(parts) != n {
		return nil, fmt.Errorf("%w: malformed page token", domain.ErrInvalid)
	}
	return parts, nil
}

// Next trims rows to size and returns the token of the following page: the
// key of the last row kept when there are more rows than size, and empty
// otherwise. A caller fetches size+1 rows to find out.
func Next[T any](rows []T, size int32, key func(T) []string) ([]T, string) {
	if int32(len(rows)) <= size { //nolint:gosec // G115: a page never holds 2^31 rows
		return rows, ""
	}
	rows = rows[:size]
	return rows, Encode(key(rows[size-1])...)
}
