package store

import "errors"

// Sentinel errors. Handlers translate each to a specific HTTP status +
// poka-yoke response per CORE_GUIDELINES §12.
var (
	// ErrNotFound — key (or item) does not exist.
	ErrNotFound = errors.New("store: not found")

	// ErrVersionMismatch — caller supplied _meta.version did not match
	// the current version on disk. Handlers translate to 412 with
	// the current envelope embedded.
	ErrVersionMismatch = errors.New("store: version mismatch")

	// ErrVersionRequired — caller wrote without a version against an
	// existing key. Handlers translate to 409 VERSION_REQUIRED.
	ErrVersionRequired = errors.New("store: version required (key already exists)")

	// ErrWrongShape — caller used an op that doesn't match the key's
	// established shape (e.g. APPEND on a singleton). Handlers translate
	// to 409 WRONG_SHAPE.
	ErrWrongShape = errors.New("store: wrong shape for this key")

	// ErrCASMismatch — CAS expected value did not equal current value.
	// Handlers translate to 409 CAS_MISMATCH with current value.
	ErrCASMismatch = errors.New("store: cas mismatch")

	// ErrLineTooLong — an APPEND would write a line exceeding the
	// PIPE_BUF threshold (4 KB on Linux). Handlers translate to 413.
	ErrLineTooLong = errors.New("store: stream line exceeds atomic write threshold")

	// ErrInvalidValue — value is not valid JSON, or violates a per-op
	// constraint (INCREMENT on a non-number, MERGE on a non-object).
	ErrInvalidValue = errors.New("store: invalid value for operation")
)

// ConflictError is returned when a write fails CAS; carries the current
// envelope so handlers can embed it in the 412 response without needing
// a follow-up read.
type ConflictError struct {
	Current     *Envelope
	YourVersion string
}

func (e *ConflictError) Error() string { return ErrVersionMismatch.Error() }
func (e *ConflictError) Unwrap() error { return ErrVersionMismatch }

// CASError is returned when a CAS expected-value comparison fails.
type CASError struct {
	Current *Envelope
}

func (e *CASError) Error() string { return ErrCASMismatch.Error() }
func (e *CASError) Unwrap() error { return ErrCASMismatch }

// WrongShapeError carries the actual + attempted shape so handlers can
// build a corrective message ("this key is a stream; use :append").
type WrongShapeError struct {
	Key     string
	Actual  string // current shape on disk
	Attempt string // shape implied by the op the caller used
}

func (e *WrongShapeError) Error() string { return ErrWrongShape.Error() }
func (e *WrongShapeError) Unwrap() error { return ErrWrongShape }
