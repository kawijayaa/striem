// Package sqlast defines the dialect-neutral SQL representation emitted by
// the KQL compiler.
package sqlast

// Query is a SQL query expression.
type Query interface {
	queryNode()
}

// Select is one SQL SELECT query block.
type Select struct {
	Distinct    bool
	Projections []SelectItem
	From        Source
	Where       Expr
	GroupBy     []Expr
	OrderBy     []Order
	Limit       Expr
}

func (*Select) queryNode() {}

// Union combines query operands. All selects UNION ALL when All is true.
type Union struct {
	All     bool
	Queries []Query
}

func (*Union) queryNode() {}

type SelectItem struct {
	Expr  Expr
	Alias string
}

type Order struct {
	Expr      Expr
	Direction string
	Nulls     string
}

// Source is a FROM-clause relation.
type Source interface {
	sourceNode()
}

type Table struct {
	Parts []string
	Alias string
}

func (*Table) sourceNode() {}

type Subquery struct {
	Query Query
	Alias string
}

func (*Subquery) sourceNode() {}

type Join struct {
	Left  Source
	Right Source
	Kind  string
	On    Expr
	Using []string
}

func (*Join) sourceNode() {}

type Values struct {
	Columns []string
	Rows    [][]Expr
	Alias   string
}

func (*Values) sourceNode() {}

type Series struct {
	Column string
	From   Expr
	To     Expr
	Step   Expr
	Alias  string
}

func (*Series) sourceNode() {}

// JSONEach expands one JSON array value into value/key rows.
type JSONEach struct {
	Value Expr
	Alias string
}

func (*JSONEach) sourceNode() {}

// Expr is a SQL scalar expression.
type Expr interface {
	exprNode()
}

type Identifier struct{ Parts []string }
type Literal struct {
	Kind  LiteralKind
	Value string
}
type Parameter struct {
	Index int
	Kind  LiteralKind
}
type Star struct{}
type QualifiedStar struct{ Parts []string }
type Random struct{}
type Unary struct {
	Operator string
	Operand  Expr
}
type Binary struct {
	Left     Expr
	Operator string
	Right    Expr
}
type Call struct {
	Name string
	Args []Expr
}

// Window applies a SQL window function to a partition and ordering.
type Window struct {
	Expr        Expr
	PartitionBy []Expr
	OrderBy     []Order
}
type List struct{ Items []Expr }
type Case struct {
	Branches []When
	Else     Expr
}
type When struct {
	Condition Expr
	Result    Expr
}
type Cast struct {
	Expr Expr
	Type string
	Safe bool
}
type Index struct {
	Value Expr
	Index Expr
}
type Regex struct {
	Value         Expr
	Pattern       Expr
	CaseSensitive bool
}
type Exists struct{ Query Query }
type Filtered struct {
	Expr  Expr
	Where Expr
}

func (*Identifier) exprNode()    {}
func (*Literal) exprNode()       {}
func (*Parameter) exprNode()     {}
func (*Star) exprNode()          {}
func (*QualifiedStar) exprNode() {}
func (*Random) exprNode()        {}
func (*Unary) exprNode()         {}
func (*Binary) exprNode()        {}
func (*Window) exprNode()        {}
func (*Call) exprNode()          {}
func (*List) exprNode()          {}
func (*Case) exprNode()          {}
func (*Cast) exprNode()          {}
func (*Index) exprNode()         {}
func (*Regex) exprNode()         {}
func (*Exists) exprNode()        {}
func (*Filtered) exprNode()      {}

type LiteralKind uint8

const (
	NullLiteral LiteralKind = iota
	NumberLiteral
	StringLiteral
	BooleanLiteral
	DateTimeLiteral
	IntervalLiteral
	JSONLiteral
)
