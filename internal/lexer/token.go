package lexer

type TokenKind string

const (
	TokenIdentifier TokenKind = "identifier"
	TokenNumber     TokenKind = "number"
	TokenString     TokenKind = "string"
	TokenPrompt     TokenKind = "prompt"

	TokenDef      TokenKind = "def"
	TokenMacro    TokenKind = "macro"
	TokenImport   TokenKind = "import"
	TokenFrom     TokenKind = "from"
	TokenAs       TokenKind = "as"
	TokenIf       TokenKind = "if"
	TokenElif     TokenKind = "elif"
	TokenElse     TokenKind = "else"
	TokenMatch    TokenKind = "match"
	TokenCase     TokenKind = "case"
	TokenWhile    TokenKind = "while"
	TokenWith     TokenKind = "with"
	TokenTry      TokenKind = "try"
	TokenExcept   TokenKind = "except"
	TokenFinally  TokenKind = "finally"
	TokenDefer    TokenKind = "defer"
	TokenAssert   TokenKind = "assert"
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
	TokenDot      TokenKind = "."
	TokenColon    TokenKind = ":"
	TokenArrow    TokenKind = "->"
	TokenAssign   TokenKind = "="
	TokenPlus     TokenKind = "+"
	TokenMinus    TokenKind = "-"
	TokenStar     TokenKind = "*"
	TokenSlash    TokenKind = "/"
	TokenPercent  TokenKind = "%"
	TokenAt       TokenKind = "@"
	TokenEq       TokenKind = "=="
	TokenNotEq    TokenKind = "!="
	TokenLT       TokenKind = "<"
	TokenLTE      TokenKind = "<="
	TokenGT       TokenKind = ">"
	TokenGTE      TokenKind = ">="
)

var keywords = map[string]TokenKind{
	"def":      TokenDef,
	"macro":    TokenMacro,
	"import":   TokenImport,
	"from":     TokenFrom,
	"as":       TokenAs,
	"if":       TokenIf,
	"elif":     TokenElif,
	"else":     TokenElse,
	"match":    TokenMatch,
	"case":     TokenCase,
	"while":    TokenWhile,
	"with":     TokenWith,
	"try":      TokenTry,
	"except":   TokenExcept,
	"finally":  TokenFinally,
	"defer":    TokenDefer,
	"assert":   TokenAssert,
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
