// Package id converts between a person's integer primary key and the
// canonical "X-000123" string Xensus shows at every API and UI boundary.
// Persons are immortal and their IDs are never reused, so the printed
// form is the stable public handle for a person across all of Xensus.
package id

import (
	"fmt"
	"strconv"
	"strings"
)

// Format renders an integer primary key as the canonical "X-000123"
// string. IDs below six digits are zero-padded; larger IDs print in
// full ("X-1000000").
func Format(n int64) string {
	return fmt.Sprintf("X-%06d", n)
}

// Parse accepts either the canonical form ("X-000123", case-insensitive,
// dash optional) or a bare integer string ("123") and returns the
// underlying ID. Surrounding whitespace is tolerated. The result is
// always positive; zero, negative, and non-numeric inputs are rejected.
func Parse(s string) (int64, error) {
	body := strings.TrimSpace(s)
	if body == "" {
		return 0, fmt.Errorf("id is empty")
	}
	if body[0] == 'X' || body[0] == 'x' {
		body = strings.TrimPrefix(body[1:], "-")
	}
	n, err := strconv.ParseInt(body, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid id %q: not a number", s)
	}
	if n <= 0 {
		return 0, fmt.Errorf("invalid id %q: must be positive", s)
	}
	return n, nil
}
