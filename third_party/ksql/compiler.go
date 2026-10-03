package ksql

import (
	"fmt"

	"github.com/kawijayaa/ksql/dialect"
	"github.com/kawijayaa/ksql/kql"
	"github.com/kawijayaa/ksql/sqlast"
)

// Compiler translates KQL into one configured SQL dialect. A Compiler is safe
// for concurrent use after construction.
type Compiler struct {
	dialect            dialect.Dialect
	functionRules      map[string]FunctionRule
	typedFunctionRules map[string]TypedFunctionRule
	operatorRules      map[string]OperatorRule
	sourceRules        map[string]SourceRule
	catalog            Catalog
	parameterization   ParameterizationMode
	limits             Limits
}

// New constructs a compiler for target. A nil target selects ANSI SQL.
func New(target dialect.Dialect, options ...Option) *Compiler {
	if target == nil {
		target = dialect.ANSI()
	}
	compiler := &Compiler{
		dialect: target, functionRules: make(map[string]FunctionRule), typedFunctionRules: make(map[string]TypedFunctionRule),
		operatorRules: make(map[string]OperatorRule), sourceRules: make(map[string]SourceRule),
	}
	for _, option := range options {
		if option != nil {
			option(compiler)
		}
	}
	return compiler
}

// Result contains either SQL or error diagnostics. SQL is empty whenever an
// error diagnostic is present.
type Result struct {
	SQL         string
	Args        []any
	Tree        *kql.Script
	Query       sqlast.Query
	Diagnostics []Diagnostic
	Features    []FeatureUse
	Columns     []Column
}

// OK reports whether compilation completed without an error.
func (r Result) OK() bool {
	for _, diagnostic := range r.Diagnostics {
		if diagnostic.Severity == SeverityError {
			return false
		}
	}
	return true
}

// Compile parses and translates source. The method never emits partial SQL.
func (c *Compiler) Compile(source string) Result {
	if c.limits.MaxSourceBytes > 0 && len(source) > c.limits.MaxSourceBytes {
		return Result{Diagnostics: []Diagnostic{{Code: "KQLB0001", Severity: SeverityError, Phase: PhaseBind, Message: fmt.Sprintf("query exceeds MaxSourceBytes (%d)", c.limits.MaxSourceBytes), Span: kql.Span{Offset: 0, Length: len(source), Line: 1, Column: 1}}}}
	}
	tree, parseErrors := kql.Parse(source)
	result := Result{Tree: tree}
	for _, parseError := range parseErrors {
		result.Diagnostics = append(result.Diagnostics, Diagnostic{
			Code: parseError.Code, Severity: SeverityError, Phase: PhaseParse,
			Message: parseError.Message, Span: parseError.Span,
		})
	}
	if !result.OK() {
		return result
	}

	state := compileState{compiler: c, scalarLets: make(map[string]boundExpr), tabularLets: make(map[string]Relation), functions: make(map[string]*boundFunction)}
	var relation Relation
	for _, statement := range tree.Statements {
		switch node := statement.(type) {
		case *kql.LetStatement:
			state.bindLet(node, nil)
		case *kql.ControlStatement:
			state.error("KQLL0102", node.Span, "statement."+node.Kind, node.Kind+" is a KQL session/control statement with no SQL query equivalent")
		case *kql.ExpressionStatement:
			if relation.Query != nil {
				state.error("KQLL0103", node.Span, "statement.expression", "multiple result-producing statements cannot be represented by one SQL query")
				continue
			}
			relation = state.pipeline(node.Pipeline)
		}
	}
	relation.Query = state.materializeScalars(relation.Query)
	result.Diagnostics = state.diagnostics
	result.Features = state.features
	if !result.OK() || relation.Query == nil {
		return result
	}
	state.args = sqlast.NormalizeParameters(relation.Query, state.args)
	result.Args = append([]any(nil), state.args...)

	sql, err := dialect.Render(c.dialect, relation.Query)
	if err != nil {
		result.Diagnostics = append(result.Diagnostics, Diagnostic{
			Code: "SQLD0001", Severity: SeverityError, Phase: PhaseSQL,
			Message: err.Error(),
		})
		return result
	}
	result.SQL = sql
	result.Query = relation.Query
	if !relation.Schema.Unknown {
		result.Columns = append([]Column(nil), relation.Schema.Columns...)
	}
	return result
}
