package api

import (
	"fmt"
	"time"
)

// Kind sorts every failure into the buckets the exit codes are built on, so
// the CLI never has to read an error message to decide what happened.
type Kind int

const (
	KindGeneric    Kind = iota // exit 1
	KindUsage                  // exit 2: input obramat.es cannot take
	KindAuth                   // exit 3: unused today, the CLI needs no account
	KindNotFound               // exit 4
	KindBlocked                // exit 5: DataDome refusal or HTTP 429
	KindNetwork                // exit 6: transport failure, timeout, 5xx
	KindAPIChanged             // exit 7: a known page no longer has the expected shape
)

var kindNames = map[Kind]string{
	KindGeneric:    "generic",
	KindUsage:      "usage",
	KindAuth:       "auth",
	KindNotFound:   "not_found",
	KindBlocked:    "blocked",
	KindNetwork:    "network",
	KindAPIChanged: "api_changed",
}

func (k Kind) String() string { return kindNames[k] }

// ExitCode is the process status for this kind (docs/cli-spec.md, exit codes).
// Spelled out, so inserting a kind cannot silently renumber the others.
func (k Kind) ExitCode() int {
	switch k {
	case KindGeneric:
		return 1
	case KindUsage:
		return 2
	case KindAuth:
		return 3
	case KindNotFound:
		return 4
	case KindBlocked:
		return 5
	case KindNetwork:
		return 6
	case KindAPIChanged:
		return 7
	}
	return 1
}

// Error is what the api package returns for anything that is not a Go
// programming error. Body is already trimmed and safe to print.
type Error struct {
	Kind      Kind
	Status    int
	Endpoint  string
	Body      string
	Msg       string
	Retryable bool
	// RetryAfter is a 429's Retry-After wait; RetryAfterSet says the header
	// was there at all, since 0 is a valid value.
	RetryAfter    time.Duration
	RetryAfterSet bool
}

func (e *Error) Error() string { return e.Msg }

func usagef(format string, args ...any) *Error {
	return &Error{Kind: KindUsage, Msg: fmt.Sprintf(format, args...)}
}

func notFoundf(format string, args ...any) *Error {
	return &Error{Kind: KindNotFound, Msg: fmt.Sprintf(format, args...)}
}

// changed reports a page whose shape moved. endpoint is a path template with
// no user input in it, so it can go into a public issue link.
func changed(endpoint, why string, body []byte) *Error {
	return &Error{Kind: KindAPIChanged, Endpoint: endpoint, Body: excerpt(body),
		Msg: fmt.Sprintf("obramat.es answered %s with something this cli does not understand (%s)", endpoint, why)}
}

func excerpt(b []byte) string {
	const n = 300
	if len(b) > n {
		b = b[:n]
	}
	return string(b)
}
