package ast

type Program struct {
	Statements []Stmt
}

type TypeRef struct {
	Expr string
}

func (t TypeRef) String() string {
	if t.Expr == "" {
		return "any"
	}
	return t.Expr
}

type Param struct {
	Name        string
	Type        TypeRef
	Default     Expr
	DefaultText string
}

type Stmt interface {
	stmtNode()
	LineNumber() int
}

type Expr interface {
	exprNode()
	LineNumber() int
}

type FunctionDef struct {
	Line       int
	Name       string
	Params     []Param
	ReturnType TypeRef
	Body       string
}

func (*FunctionDef) stmtNode()         {}
func (s *FunctionDef) LineNumber() int { return s.Line }

type ImportStmt struct {
	Line  int
	Path  string
	Alias string
}

func (*ImportStmt) stmtNode()         {}
func (s *ImportStmt) LineNumber() int { return s.Line }

type ImportName struct {
	Name  string
	Alias string
}

type FromImportStmt struct {
	Line  int
	Path  string
	Names []ImportName
}

func (*FromImportStmt) stmtNode()         {}
func (s *FromImportStmt) LineNumber() int { return s.Line }

type AssignStmt struct {
	Line   int
	Target Expr
	Value  Expr
}

func (*AssignStmt) stmtNode()         {}
func (s *AssignStmt) LineNumber() int { return s.Line }

type ExprStmt struct {
	Line int
	Expr Expr
}

func (*ExprStmt) stmtNode()         {}
func (s *ExprStmt) LineNumber() int { return s.Line }

type IfStmt struct {
	Line      int
	Condition Expr
	Then      []Stmt
	Else      []Stmt
}

func (*IfStmt) stmtNode()         {}
func (s *IfStmt) LineNumber() int { return s.Line }

type WhileStmt struct {
	Line      int
	Condition Expr
	Body      []Stmt
}

func (*WhileStmt) stmtNode()         {}
func (s *WhileStmt) LineNumber() int { return s.Line }

type ForStmt struct {
	Line     int
	Name     string
	Iterable Expr
	Body     []Stmt
}

func (*ForStmt) stmtNode()         {}
func (s *ForStmt) LineNumber() int { return s.Line }

type BreakStmt struct {
	Line int
}

func (*BreakStmt) stmtNode()         {}
func (s *BreakStmt) LineNumber() int { return s.Line }

type ContinueStmt struct {
	Line int
}

func (*ContinueStmt) stmtNode()         {}
func (s *ContinueStmt) LineNumber() int { return s.Line }

type PassStmt struct {
	Line int
}

func (*PassStmt) stmtNode()         {}
func (s *PassStmt) LineNumber() int { return s.Line }

type Identifier struct {
	Line int
	Name string
}

func (*Identifier) exprNode()         {}
func (e *Identifier) LineNumber() int { return e.Line }

type Literal struct {
	Line  int
	Value any
}

func (*Literal) exprNode()         {}
func (e *Literal) LineNumber() int { return e.Line }

type UnaryExpr struct {
	Line     int
	Operator string
	Right    Expr
}

func (*UnaryExpr) exprNode()         {}
func (e *UnaryExpr) LineNumber() int { return e.Line }

type PromptExpr struct {
	Line int
	Text string
}

func (*PromptExpr) exprNode()         {}
func (e *PromptExpr) LineNumber() int { return e.Line }

type BinaryExpr struct {
	Line     int
	Left     Expr
	Operator string
	Right    Expr
}

func (*BinaryExpr) exprNode()         {}
func (e *BinaryExpr) LineNumber() int { return e.Line }

type CallArgument struct {
	Name  string
	Value Expr
}

type CallExpr struct {
	Line      int
	Callee    Expr
	Arguments []CallArgument
}

func (*CallExpr) exprNode()         {}
func (e *CallExpr) LineNumber() int { return e.Line }

type IndexExpr struct {
	Line  int
	Left  Expr
	Index Expr
}

func (*IndexExpr) exprNode()         {}
func (e *IndexExpr) LineNumber() int { return e.Line }

type MemberExpr struct {
	Line int
	Left Expr
	Name string
}

func (*MemberExpr) exprNode()         {}
func (e *MemberExpr) LineNumber() int { return e.Line }

type ListLiteral struct {
	Line     int
	Elements []Expr
}

func (*ListLiteral) exprNode()         {}
func (e *ListLiteral) LineNumber() int { return e.Line }

type DictItem struct {
	Key   Expr
	Value Expr
}

type DictLiteral struct {
	Line  int
	Items []DictItem
}

func (*DictLiteral) exprNode()         {}
func (e *DictLiteral) LineNumber() int { return e.Line }
