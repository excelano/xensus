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

	// ErrNameRequired is returned when a create/rename is attempted with a
	// blank name on an entity whose name is its identity (a system has no
	// portable handle, so it must be named).
	ErrNameRequired = errors.New("name is required")

	// ErrUPNRequired is returned when a steward promotion is attempted with
	// a blank UPN — a promotion is keyed by the invitee's UPN, so it can't
	// be empty.
	ErrUPNRequired = errors.New("upn is required")

	// ErrAlreadySteward is returned when a UPN is promoted but already holds
	// an active steward row. The promotion is redundant, not an error in the
	// caller's intent, so it maps to 409 Conflict rather than a 400.
	ErrAlreadySteward = errors.New("already a steward")

	// ErrAlreadyInvited is returned when a UPN is promoted but already has an
	// outstanding pending invitation waiting to be claimed at sign-in.
	ErrAlreadyInvited = errors.New("already invited")

	// ErrSelfRemoval is returned when a steward attempts to demote themselves.
	// A steward must find a successor rather than leave the deployment with no
	// one able to administer it.
	ErrSelfRemoval = errors.New("a steward cannot remove themselves")
)
