package lexer

type TokenKind string

const (
	TokenIdentifier TokenKind = "identifier"
	TokenNumber     TokenKind = "number"
	TokenString     TokenKind = "string"
	TokenPrompt     TokenKind = "prompt"

	TokenDef      TokenKind = "def"
	TokenImport   TokenKind = "import"
	TokenFrom     TokenKind = "from"
	TokenAs       TokenKind = "as"
	TokenIf       TokenKind = "if"
	TokenElif     TokenKind = "elif"
	TokenElse     TokenKind = "else"
	TokenWhile    TokenKind = "while"
	TokenFor      TokenKind = "for"
	TokenIn       TokenKind = "in"
	TokenAnd      TokenKind = "and"
	TokenOr       TokenKind = "or"
	TokenNot      TokenKind = "not"
	TokenTrue     TokenKind = "true"
	TokenFalse    TokenKind = "false"
	TokenNone     TokenKind = "none"
	TokenBreak    TokenKind = "break"
	TokenContinue TokenKind = "continue"
	TokenPass     TokenKind = "pass"

	TokenLParen   TokenKind = "("
	TokenRParen   TokenKind = ")"
	TokenLBracket TokenKind = "["
	TokenRBracket TokenKind = "]"
	TokenLBrace   TokenKind = "{"
	TokenRBrace   TokenKind = "}"
	TokenComma    TokenKind = ","
	TokenColon    TokenKind = ":"
	TokenArrow    TokenKind = "->"
	TokenAssign   TokenKind = "="
	TokenPlus     TokenKind = "+"
	TokenMinus    TokenKind = "-"
	TokenStar     TokenKind = "*"
	TokenSlash    TokenKind = "/"
	TokenPercent  TokenKind = "%"
	TokenEq       TokenKind = "=="
	TokenNotEq    TokenKind = "!="
	TokenLT       TokenKind = "<"
	TokenLTE      TokenKind = "<="
	TokenGT       TokenKind = ">"
	TokenGTE      TokenKind = ">="
)

var keywords = map[string]TokenKind{
	"def":      TokenDef,
	"import":   TokenImport,
	"from":     TokenFrom,
	"as":       TokenAs,
	"if":       TokenIf,
	"elif":     TokenElif,
	"else":     TokenElse,
	"while":    TokenWhile,
	"for":      TokenFor,
	"in":       TokenIn,
	"and":      TokenAnd,
	"or":       TokenOr,
	"not":      TokenNot,
	"true":     TokenTrue,
	"false":    TokenFalse,
	"none":     TokenNone,
	"break":    TokenBreak,
	"continue": TokenContinue,
	"pass":     TokenPass,
}

type Token struct {
	Kind   TokenKind
	Lexeme string
	Line   int
	Column int
}

type Line struct {
	Number      int
	Indent      int
	Raw         string
	Content     string
	Tokens      []Token
	Blank       bool
	CommentOnly bool
	LexError    error
}

type File struct {
	Lines []Line
}
