package ksql

import (
	"fmt"

	"github.com/kawijayaa/ksql/dialect"
	"github.com/kawijayaa/ksql/kql"
	"github.com/kawijayaa/ksql/sqlast"
)

type loweringContext struct {
	state *compileState
	input Schema
	span  kql.Span
}

func (c *loweringContext) LowerExpression(expression kql.Expression) (sqlast.Expr, error) {
	previous := c.state.input
	c.state.input = c.input
	value := c.state.expression(expression)
	c.state.input = previous
	if value.SQL == nil {
		return nil, fmt.Errorf("expression could not be lowered")
	}
	return value.SQL, nil
}

func (c *loweringContext) LowerPipeline(pipeline *kql.Pipeline) (Relation, error) {
	relation := c.state.pipeline(pipeline)
	if relation.Query == nil {
		return Relation{}, fmt.Errorf("pipeline could not be lowered")
	}
	return relation, nil
}

func (c *loweringContext) Bind(value any) sqlast.Expr { return c.state.bind(value) }

func (c *loweringContext) InputSchema() Schema { return c.input.Clone() }

func (c *loweringContext) NextAlias() string { return c.state.nextAlias() }

func (c *loweringContext) Dialect() dialect.Dialect { return c.state.compiler.dialect }

func (c *loweringContext) Catalog() Catalog { return c.state.compiler.catalog }

func (c *loweringContext) AddDiagnostic(diagnostic Diagnostic) {
	if diagnostic.Span.Length == 0 {
		diagnostic.Span = c.span
	}
	if diagnostic.Phase == "" {
		diagnostic.Phase = PhaseLower
	}
	if diagnostic.Severity == SeverityInfo {
		diagnostic.Severity = SeverityError
	}
	c.state.diagnostics = append(c.state.diagnostics, diagnostic)
}
