package core

import "errors"

// Sentinel errors live here so that downstream packages (api, web) can
// map them to HTTP status codes via errors.Is without importing every
// caller's own error type.
//
// Slices add their own sentinels as features land — only the ones in
// active use are defined now to avoid cluttering the surface with
// unused symbols.
var (
	// ErrAlreadyBound is returned when a bootstrap is attempted against
	// a deployment whose tenant_id is already set.
	ErrAlreadyBound = errors.New("xensus tenant already bound")

	// ErrNotFound is returned when an operation targets an entity (person,
	// system, association, …) that does not exist.
	ErrNotFound = errors.New("not found")
)
