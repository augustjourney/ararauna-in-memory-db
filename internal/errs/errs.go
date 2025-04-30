package errs

import "errors"

var (
	ErrNotFound         = errors.New("not found")
	ErrMalformedRESP    = errors.New("malformed RESP value")
	ErrUnknownRESPKind  = errors.New("unknown RESP kind")
)
