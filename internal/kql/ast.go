package kql

import "time"

type Query struct {
	Bindings  []LetBinding
	Source    string
	SourceAt  token
	Operators []Operator
}

type LetBinding struct {
	Name       string
	At         token
	Expression Expression
}

type Operator interface{ operatorNode() }

type WhereOperator struct {
	At         token
	Expression Expression
}

func (WhereOperator) operatorNode() {}

type NamedExpression struct {
	Name       string
	At         token
	Expression Expression
}

type ProjectOperator struct {
	At    token
	Items []NamedExpression
}

func (ProjectOperator) operatorNode() {}

type ExtendOperator struct {
	At    token
	Items []NamedExpression
}

func (ExtendOperator) operatorNode() {}

type SummarizeOperator struct {
	At         token
	Aggregates []NamedExpression
	Groups     []NamedExpression
}

func (SummarizeOperator) operatorNode() {}

type DistinctOperator struct {
	At    token
	Items []NamedExpression
}

func (DistinctOperator) operatorNode() {}

type SortTerm struct {
	Expression Expression
	Descending bool
}
type SortOperator struct {
	At    token
	Terms []SortTerm
}

func (SortOperator) operatorNode() {}

type TakeOperator struct {
	At    token
	Count Expression
}

func (TakeOperator) operatorNode() {}

type TopOperator struct {
	At    token
	Count Expression
	Term  SortTerm
}

func (TopOperator) operatorNode() {}

type CountOperator struct{ At token }

func (CountOperator) operatorNode() {}

type UnionOperator struct {
	At      token
	Queries []Query
}

func (UnionOperator) operatorNode() {}

type JoinKind string

const (
	JoinInner     JoinKind = "inner"
	JoinLeftOuter JoinKind = "leftouter"
)

type JoinKey struct {
	Name string
	At   token
}

type JoinOperator struct {
	At    token
	Kind  JoinKind
	Right Query
	Keys  []JoinKey
}

func (JoinOperator) operatorNode() {}

type Expression interface {
	expressionNode()
	position() token
}

type IdentifierExpression struct {
	At    token
	Parts []string
}

func (IdentifierExpression) expressionNode()   {}
func (e IdentifierExpression) position() token { return e.At }

type LiteralExpression struct {
	At    token
	Value any
}

func (LiteralExpression) expressionNode()   {}
func (e LiteralExpression) position() token { return e.At }

type DurationExpression struct {
	At    token
	Value time.Duration
}

func (DurationExpression) expressionNode()   {}
func (e DurationExpression) position() token { return e.At }

type UnaryExpression struct {
	At       token
	Operator string
	Operand  Expression
}

func (UnaryExpression) expressionNode()   {}
func (e UnaryExpression) position() token { return e.At }

type BinaryExpression struct {
	At          token
	Operator    string
	Left, Right Expression
}

func (BinaryExpression) expressionNode()   {}
func (e BinaryExpression) position() token { return e.At }

type ListExpression struct {
	At    token
	Items []Expression
}

func (ListExpression) expressionNode()   {}
func (e ListExpression) position() token { return e.At }

type FunctionExpression struct {
	At        token
	Name      string
	Arguments []Expression
}

func (FunctionExpression) expressionNode()   {}
func (e FunctionExpression) position() token { return e.At }
