package ksql

import (
	"fmt"
	"strings"

	"github.com/kawijayaa/ksql/dialect"
	"github.com/kawijayaa/ksql/kql"
	"github.com/kawijayaa/ksql/sqlast"
)

// Option configures a Compiler during construction.
type Option func(*Compiler)

// FunctionRule lowers one KQL function call. Rules receive already-lowered
// arguments and must return a dialect-neutral SQL expression.
type FunctionRule func(arguments []sqlast.Expr) (sqlast.Expr, error)

// LoweringContext exposes safe, per-compilation services to extension rules.
// A context must not be retained after a rule returns.
type LoweringContext interface {
	LowerExpression(kql.Expression) (sqlast.Expr, error)
	LowerPipeline(*kql.Pipeline) (Relation, error)
	Bind(value any) sqlast.Expr
	InputSchema() Schema
	NextAlias() string
	AddDiagnostic(Diagnostic)
	Dialect() dialect.Dialect
	Catalog() Catalog
}

// OperatorRule lowers a recognized KQL tabular operator.
type OperatorRule func(context LoweringContext, input Relation, operator kql.Operator) (Relation, error)

// SourceRule lowers a recognized KQL tabular source.
type SourceRule func(context LoweringContext, source kql.Source) (Relation, error)

// ParameterizationMode controls rendering of user-provided scalar literals.
type ParameterizationMode uint8

const (
	// LiteralParameters renders literals directly using dialect-safe quoting.
	LiteralParameters ParameterizationMode = iota
	// BoundParameters renders scalar literals as placeholders and returns their
	// values in Result.Args.
	BoundParameters
)

// WithFunction registers an explicit KQL function mapping. It overrides a
// built-in mapping with the same name. Invalid or empty names are ignored.
func WithFunction(name string, rule FunctionRule) Option {
	return func(compiler *Compiler) {
		name = strings.ToLower(strings.TrimSpace(name))
		if name != "" && rule != nil {
			compiler.functionRules[name] = rule
		}
	}
}

// WithOperator registers a target-specific tabular operator adapter.
func WithOperator(name string, rule OperatorRule) Option {
	return func(compiler *Compiler) {
		name = strings.ToLower(strings.TrimSpace(name))
		if name != "" && rule != nil {
			compiler.operatorRules[name] = rule
		}
	}
}

// WithSource registers a target-specific tabular source adapter.
func WithSource(kind string, rule SourceRule) Option {
	return func(compiler *Compiler) {
		kind = strings.ToLower(strings.TrimSpace(kind))
		if kind != "" && rule != nil {
			compiler.sourceRules[kind] = rule
		}
	}
}

// WithCatalog enables case-insensitive schema binding and table validation.
func WithCatalog(catalog Catalog) Option {
	return func(compiler *Compiler) { compiler.catalog = catalog }
}

// WithParameterization selects literal or secure placeholder rendering.
func WithParameterization(mode ParameterizationMode) Option {
	return func(compiler *Compiler) { compiler.parameterization = mode }
}

// WithParameters enables secure placeholder rendering for KQL scalar values.
func WithParameters() Option { return WithParameterization(BoundParameters) }

// WithLimits configures compiler resource limits. Negative values are treated
// as zero (unlimited).
func WithLimits(limits Limits) Option {
	return func(compiler *Compiler) { compiler.limits = limits }
}

// SQLFunction maps a KQL call to a SQL function with the supplied name.
// Callers should use this only after verifying semantic compatibility for the
// configured database or after installing a compatible UDF.
func SQLFunction(name string) FunctionRule {
	return func(arguments []sqlast.Expr) (sqlast.Expr, error) {
		name = strings.TrimSpace(name)
		if !validSQLName(name) {
			return nil, fmt.Errorf("invalid SQL function name %q", name)
		}
		return &sqlast.Call{Name: name, Args: arguments}, nil
	}
}

func validSQLName(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		if r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || i > 0 && r >= '0' && r <= '9' {
			continue
		}
		return false
	}
	return true
}
