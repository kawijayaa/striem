package ksql

import (
	"fmt"

	"github.com/kawijayaa/ksql/kql"
)

// Severity is a diagnostic's effect on compilation.
type Severity uint8

const (
	SeverityInfo Severity = iota
	SeverityWarning
	SeverityError
)

// Phase identifies the compiler stage that produced a diagnostic.
type Phase string

const (
	PhaseParse Phase = "parse"
	PhaseBind  Phase = "bind"
	PhaseLower Phase = "lower"
	PhaseSQL   Phase = "sql"
)

// Diagnostic is a stable, source-located compilation message.
type Diagnostic struct {
	Code     string
	Severity Severity
	Phase    Phase
	Message  string
	Span     kql.Span
	Feature  string
}

func (d Diagnostic) Error() string {
	return fmt.Sprintf("%s: %s at %s", d.Code, d.Message, d.Span)
}

// FeatureUse records a KQL feature encountered during compilation.
type FeatureUse struct {
	ID      string
	Support string
	Span    kql.Span
}
