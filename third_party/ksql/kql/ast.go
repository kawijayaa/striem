package kql

// Script is a semicolon-delimited KQL statement list.
type Script struct {
	Source     string
	Tokens     []Token
	Statements []Statement
}

// Node is implemented by all syntax nodes.
type Node interface {
	NodeSpan() Span
}

// Statement is a top-level KQL statement.
type Statement interface {
	Node
	statementNode()
}

// ExpressionStatement evaluates a scalar or tabular expression.
type ExpressionStatement struct {
	Pipeline *Pipeline
	Span     Span
}

func (*ExpressionStatement) statementNode()   {}
func (s *ExpressionStatement) NodeSpan() Span { return s.Span }

// LetStatement binds a scalar, tabular expression, or function declaration.
type LetStatement struct {
	Name     string
	Value    Expression
	Pipeline *Pipeline
	Raw      []Token
	Span     Span
}

func (*LetStatement) statementNode()   {}
func (s *LetStatement) NodeSpan() Span { return s.Span }

// ControlStatement represents a parsed query/session statement that does not
// directly produce a SQL relation: alias, declare, restrict, or set.
type ControlStatement struct {
	Kind string
	Raw  []Token
	Span Span
}

func (*ControlStatement) statementNode()   {}
func (s *ControlStatement) NodeSpan() Span { return s.Span }

// Pipeline consists of a source followed by zero or more tabular operators.
type Pipeline struct {
	Source    Source
	Operators []Operator
	Span      Span
}

func (p *Pipeline) NodeSpan() Span { return p.Span }

// Source is the left-most pipeline input. Kind is table, print, range,
// datatable, union, externaldata, inline_external_table, entity_group, or expression.
type Source struct {
	Union   *UnionSpec
	Kind    string
	Name    string
	Expr    Expression
	Items   []Expression
	Columns []ColumnDefinition
	Rows    [][]Expression
	From    Expression
	To      Expression
	Step    Expression
	Raw     []Token
	Span    Span
}

type ColumnDefinition struct {
	Name string
	Type string
}

func (s Source) NodeSpan() Span { return s.Span }

// Operator is a tabular pipeline operator. Body is one of the typed specs
// below, or RawSpec for recognized syntax not yet semantically lowered.
type Operator struct {
	Kind string
	Body OperatorBody
	Raw  []Token
	Span Span
}

func (o Operator) NodeSpan() Span { return o.Span }

type OperatorBody interface {
	operatorBody()
}

type ExpressionSpec struct{ Expressions []Expression }
type SummarizeSpec struct {
	Aggregates []Expression
	By         []Expression
}
type OrderedExpression struct {
	Expression Expression
	Direction  string
	Nulls      string
}
type SortSpec struct{ Expressions []OrderedExpression }
type TopSpec struct {
	Count Expression
	By    OrderedExpression
}
type JoinSpec struct {
	Kind       string
	Right      *Pipeline
	Conditions []Expression
	Condition  Expression
}
type UnionSpec struct {
	Kind   string
	Inputs []*Pipeline
}
type MvExpansion struct {
	Name       string
	Expression Expression
	Type       string
	Span       Span
}
type MvExpandSpec struct {
	Kind      string
	ItemIndex string
	Items     []MvExpansion
	Limit     Expression
	Legacy    bool
}
type MvApplySpec struct {
	ItemIndex string
	Items     []MvExpansion
	Limit     Expression
	Operators []Operator
}
type AsSpec struct {
	Name         string
	Materialized *bool
}
type SampleDistinctSpec struct {
	Count  Expression
	Column Expression
}

// SearchSpec contains the scalar term searched across the visible schema.
type SearchSpec struct{ Term Expression }
type RawSpec struct{ Tokens []Token }

func (ExpressionSpec) operatorBody()     {}
func (SummarizeSpec) operatorBody()      {}
func (SortSpec) operatorBody()           {}
func (TopSpec) operatorBody()            {}
func (JoinSpec) operatorBody()           {}
func (UnionSpec) operatorBody()          {}
func (MvExpandSpec) operatorBody()       {}
func (MvApplySpec) operatorBody()        {}
func (AsSpec) operatorBody()             {}
func (SampleDistinctSpec) operatorBody() {}
func (SearchSpec) operatorBody()         {}
func (RawSpec) operatorBody()            {}

// Expression is a scalar KQL expression.
type Expression interface {
	Node
	expressionNode()
}

// ColumnPatternExpression is only valid in column-selection operators.
type ColumnPatternExpression struct {
	Pattern string
	Order   string
	Span    Span
}

func (*ColumnPatternExpression) expressionNode()  {}
func (e *ColumnPatternExpression) NodeSpan() Span { return e.Span }

type NameExpression struct {
	Name string
	Span Span
}
type LiteralExpression struct {
	Text string
	Kind TokenKind
	Span Span
}
type StarExpression struct{ Span Span }
type UnaryExpression struct {
	Operator string
	Operand  Expression
	Span     Span
}
type BinaryExpression struct {
	Left     Expression
	Operator string
	Right    Expression
	Span     Span
}
type CallExpression struct {
	Function Expression
	Args     []Expression
	Span     Span
}
type IndexExpression struct {
	Value Expression
	Index Expression
	Span  Span
}
type NamedExpression struct {
	Name  string
	Value Expression
	Span  Span
}
type ListExpression struct {
	Items []Expression
	Span  Span
}
type RawExpression struct {
	Tokens []Token
	Span   Span
}

func (*NameExpression) expressionNode()    {}
func (*LiteralExpression) expressionNode() {}
func (*StarExpression) expressionNode()    {}
func (*UnaryExpression) expressionNode()   {}
func (*BinaryExpression) expressionNode()  {}
func (*CallExpression) expressionNode()    {}
func (*IndexExpression) expressionNode()   {}
func (*NamedExpression) expressionNode()   {}
func (*ListExpression) expressionNode()    {}
func (*RawExpression) expressionNode()     {}

func (e *NameExpression) NodeSpan() Span    { return e.Span }
func (e *LiteralExpression) NodeSpan() Span { return e.Span }
func (e *StarExpression) NodeSpan() Span    { return e.Span }
func (e *UnaryExpression) NodeSpan() Span   { return e.Span }
func (e *BinaryExpression) NodeSpan() Span  { return e.Span }
func (e *CallExpression) NodeSpan() Span    { return e.Span }
func (e *IndexExpression) NodeSpan() Span   { return e.Span }
func (e *NamedExpression) NodeSpan() Span   { return e.Span }
func (e *ListExpression) NodeSpan() Span    { return e.Span }
func (e *RawExpression) NodeSpan() Span     { return e.Span }
