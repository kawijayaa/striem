package ksql

import (
	"fmt"
	"strings"

	featureledger "github.com/kawijayaa/ksql/features"
	"github.com/kawijayaa/ksql/kql"
	"github.com/kawijayaa/ksql/sqlast"
)

type compileState struct {
	compiler           *Compiler
	scalarLets         map[string]boundExpr
	tabularLets        map[string]Relation
	functions          map[string]*boundFunction
	functionDepth      int
	functionValidation bool
	functionWork       int
	functionSQLWork    int
	scalarBindings     []scalarBinding
	scalarSQLWork      int
	rowID              int
	nextRowID          int
	rowOrigins         map[*sqlast.Identifier]int
	capturedRows       map[int]struct{}
	blockedRows        map[string]bool
	diagnostics        []Diagnostic
	features           []FeatureUse
	args               []any
	alias              int
	defaultColumn      int
	qualifiers         map[string]relationScope
	qualifier          string
	input              Schema
}

type boundExpr struct {
	SQL  sqlast.Expr
	Type ScalarType
	Name string
	rows map[int]struct{}
}

type relationScope struct {
	alias  string
	schema Schema
}

func (s *compileState) pipeline(pipeline *kql.Pipeline) (result Relation) {
	previousRow, previousCaptures := s.rowID, s.capturedRows
	s.nextRowID++
	s.rowID, s.capturedRows = s.nextRowID, nil
	defer func() {
		dependencies := mergeRowDependencies(result.rowDependencies, s.capturedRows)
		delete(dependencies, s.rowID)
		result.rowDependencies = dependencies
		s.rowID, s.capturedRows = previousRow, mergeRowDependencies(previousCaptures, dependencies)
	}()

	if pipeline == nil {
		return Relation{}
	}
	relation := s.source(pipeline.Source)
	relation.rowDependencies = mergeRowDependencies(relation.rowDependencies, s.capturedRows)
	delete(relation.rowDependencies, s.rowID)
	for _, operator := range pipeline.Operators {
		if relation.Query == nil {
			return Relation{}
		}
		relation = s.operator(relation, operator)
		relation.rowDependencies = mergeRowDependencies(relation.rowDependencies, s.capturedRows)
		delete(relation.rowDependencies, s.rowID)
	}
	return relation
}

// A name-valued let can refer to either a scalar binding or a relation. Resolve
// local bindings before catalog tables, and lower tables through the normal
// source rule so application filtering and typed schemas remain attached.
func (s *compileState) tabularLetValue(expression kql.Expression) (Relation, bool) {
	if call, ok := expression.(*kql.CallExpression); ok {
		if name, ok := call.Function.(*kql.NameExpression); ok {
			if function := s.functions[strings.ToLower(unquoteKQL(name.Name))]; function != nil && function.tabular {
				return s.callFunctionValue(function, call, nil).table, true
			}
		}
	}
	name, ok := expression.(*kql.NameExpression)
	if !ok {
		return Relation{}, false
	}
	logicalName := unquoteKQL(name.Name)
	key := strings.ToLower(logicalName)
	if _, scalar := s.scalarLets[key]; scalar {
		return Relation{}, false
	}
	if relation, tabular := s.tabularLets[key]; tabular {
		return relation, true
	}
	if function := s.functions[key]; function != nil && function.tabular {
		return s.callFunctionValue(function, &kql.CallExpression{Function: name, Span: name.Span}, nil).table, true
	}
	// An unquoted dotted expression is property access, not a table name.
	if logicalName == name.Name && strings.Contains(name.Name, ".") {
		return Relation{}, false
	}
	if s.compiler.catalog != nil {
		if _, found := s.compiler.catalog.ResolveTable(logicalName); !found {
			return Relation{}, false
		}
	}
	// Without a catalog, match the existing open-schema table source behavior.
	return s.source(kql.Source{Kind: "table", Name: logicalName, Expr: name, Span: name.Span}), true
}

func (s *compileState) nextAlias() string {
	alias := fmt.Sprintf("_k%d", s.alias)
	s.alias++
	return alias
}

func (s *compileState) use(id, support string, span kql.Span) {
	if entry, ok := featureledger.Lookup(id); ok {
		support = string(entry.Support)
	} else if strings.HasPrefix(id, "function.") {
		if entry, found := featureledger.Lookup("aggregate." + strings.TrimPrefix(id, "function.")); found {
			id = entry.ID
			support = string(entry.Support)
		}
	}
	s.features = append(s.features, FeatureUse{ID: id, Support: support, Span: span})
}

func (s *compileState) error(code string, span kql.Span, feature, message string) {
	s.diagnostics = append(s.diagnostics, Diagnostic{Code: code, Severity: SeverityError, Phase: PhaseLower, Message: message, Span: span, Feature: feature})
}

func (s *compileState) warn(code string, span kql.Span, feature, message string) {
	s.diagnostics = append(s.diagnostics, Diagnostic{Code: code, Severity: SeverityWarning, Phase: PhaseLower, Message: message, Span: span, Feature: feature})
}
