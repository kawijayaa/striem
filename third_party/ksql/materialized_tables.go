package ksql

import (
	"github.com/kawijayaa/ksql/dialect"
	"github.com/kawijayaa/ksql/kql"
	"github.com/kawijayaa/ksql/sqlast"
)

func (s *compileState) materializeCall(call *kql.CallExpression) Relation {
	s.use("function.materialize", "equivalent", call.Span)
	if len(call.Args) != 1 {
		s.bindError("KQLB0524", call.Span, "function.materialize", "materialize requires one unnamed tabular expression")
		return Relation{}
	}
	if _, named := call.Args[0].(*kql.NamedExpression); named {
		s.bindError("KQLB0524", call.Span, "function.materialize", "materialize requires one unnamed tabular expression")
		return Relation{}
	}
	restore := s.withoutCallerRows("function.materialize")
	defer restore()
	relation, table := s.functionTableExpression(call.Args[0])
	if !table {
		s.bindError("KQLB0524", call.Span, "function.materialize", "materialize requires a tabular expression")
		return Relation{}
	}
	return s.materializeRelation(relation, call.Span, "function.materialize")
}

func (s *compileState) materializeRelation(relation Relation, span kql.Span, feature string) Relation {
	if relation.Query == nil {
		return Relation{}
	}
	capability, supported := s.compiler.dialect.(dialect.Materialization)
	if !supported || !capability.SupportsMaterializedBindings() {
		s.error("KQLL0520", span, feature, "table materialization requires guaranteed materialized bindings on this dialect")
		return Relation{}
	}
	if !s.functionValidation && (len(relation.rowDependencies) != 0 || len(s.capturedRows) != 0) {
		s.error("KQLL0524", span, feature, "caller-row-dependent tables require correlated materialization, which this SQL lowering does not support")
		return Relation{}
	}
	reference := &sqlast.MaterializedReference{}
	if !s.addQueryBinding(reference, relation.Query, span, feature) {
		return Relation{}
	}
	return Relation{Query: &sqlast.Select{From: reference}, Schema: relation.Schema.Clone()}
}
