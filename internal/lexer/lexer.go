package lexer

import (
	"fmt"
	"strings"
	"unicode"
)

func Lex(source string) (File, error) {
	source = strings.ReplaceAll(source, "\r\n", "\n")
	source = strings.ReplaceAll(source, "\r", "\n")

	rawLines := strings.Split(source, "\n")
	lines := make([]Line, 0, len(rawLines))

	for i, raw := range rawLines {
		number := i + 1
		indent, err := countIndent(raw, number)
		if err != nil {
			return File{}, err
		}

		content := raw[indent:]
		if strings.TrimSpace(content) == "" {
			lines = append(lines, Line{
				Number:  number,
				Indent:  indent,
				Raw:     raw,
				Content: content,
				Blank:   true,
			})
			continue
		}

		tokens, commentOnly, err := tokenize(content, number, indent)
		lineEntry := Line{
			Number:      number,
			Indent:      indent,
			Raw:         raw,
			Content:     content,
			CommentOnly: commentOnly,
		}
		if err != nil {
			lineEntry.LexError = err
		} else {
			lineEntry.Tokens = tokens
		}

		lines = append(lines, lineEntry)
	}

	return File{Lines: lines}, nil
}

func countIndent(raw string, line int) (int, error) {
	indent := 0
	for indent < len(raw) {
		switch raw[indent] {
		case ' ':
			indent++
		case '\t':
			return 0, fmt.Errorf("line %d: tabs are not supported for indentation", line)
		default:
			return indent, nil
		}
	}
	return indent, nil
}

func tokenize(content string, line, indent int) ([]Token, bool, error) {
	tokens := make([]Token, 0, 8)

	for i := 0; i < len(content); {
		ch := content[i]
		if unicode.IsSpace(rune(ch)) {
			i++
			continue
		}

		column := indent + i + 1

		if ch == '#' {
			return tokens, len(tokens) == 0, nil
		}

		if ch == '*' && canStartPrompt(tokens) {
			promptText := strings.TrimSpace(content[i+1:])
			tokens = append(tokens, Token{Kind: TokenPrompt, Lexeme: promptText, Line: line, Column: column})
			return tokens, false, nil
		}

		if isIdentStart(ch) {
			start := i
			i++
			for i < len(content) && isIdentPart(content[i]) {
				i++
			}
			lexeme := content[start:i]
			kind := TokenIdentifier
			if keyword, ok := keywords[lexeme]; ok {
				kind = keyword
			}
			tokens = append(tokens, Token{Kind: kind, Lexeme: lexeme, Line: line, Column: column})
			continue
		}

		if isDigit(ch) {
			start := i
			hasDot := false
			i++
			for i < len(content) {
				switch {
				case isDigit(content[i]):
					i++
				case content[i] == '.' && !hasDot:
					hasDot = true
					i++
				default:
					goto numberDone
				}
			}
		numberDone:
			lexeme := content[start:i]
			tokens = append(tokens, Token{Kind: TokenNumber, Lexeme: lexeme, Line: line, Column: column})
			continue
		}

		if ch == '"' || ch == '\'' {
			start := i
			quote := ch
			i++
			escaped := false
			terminated := false
		stringLoop:
			for i < len(content) {
				if escaped {
					escaped = false
					i++
					continue
				}
				switch content[i] {
				case '\\':
					escaped = true
					i++
				case quote:
					i++
					terminated = true
					break stringLoop
				default:
					i++
				}
			}
			if terminated {
				tokens = append(tokens, Token{Kind: TokenString, Lexeme: content[start:i], Line: line, Column: column})
				continue
			}
			return nil, false, fmt.Errorf("line %d:%d: unterminated string literal", line, column)
		}

		if i+1 < len(content) {
			two := content[i : i+2]
			switch two {
			case "->":
				tokens = append(tokens, Token{Kind: TokenArrow, Lexeme: two, Line: line, Column: column})
				i += 2
				continue
			case "==":
				tokens = append(tokens, Token{Kind: TokenEq, Lexeme: two, Line: line, Column: column})
				i += 2
				continue
			case "!=":
				tokens = append(tokens, Token{Kind: TokenNotEq, Lexeme: two, Line: line, Column: column})
				i += 2
				continue
			case "<=":
				tokens = append(tokens, Token{Kind: TokenLTE, Lexeme: two, Line: line, Column: column})
				i += 2
				continue
			case ">=":
				tokens = append(tokens, Token{Kind: TokenGTE, Lexeme: two, Line: line, Column: column})
				i += 2
				continue
			}
		}

		kind, ok := singleCharToken(ch)
		if !ok {
			return nil, false, fmt.Errorf("line %d:%d: unexpected character %q", line, column, ch)
		}
		tokens = append(tokens, Token{Kind: kind, Lexeme: string(ch), Line: line, Column: column})
		i++
	}

	return tokens, false, nil
}

func canStartPrompt(tokens []Token) bool {
	if len(tokens) == 0 {
		return true
	}

	switch tokens[len(tokens)-1].Kind {
	case TokenAssign, TokenIf, TokenElif, TokenWhile, TokenIn:
		return true
	default:
		return false
	}
}

func singleCharToken(ch byte) (TokenKind, bool) {
	switch ch {
	case '(':
		return TokenLParen, true
	case ')':
		return TokenRParen, true
	case '[':
		return TokenLBracket, true
	case ']':
		return TokenRBracket, true
	case '{':
		return TokenLBrace, true
	case '}':
		return TokenRBrace, true
	case ',':
		return TokenComma, true
	case '.':
		return TokenDot, true
	case ':':
		return TokenColon, true
	case '=':
		return TokenAssign, true
	case '+':
		return TokenPlus, true
	case '-':
		return TokenMinus, true
	case '*':
		return TokenStar, true
	case '/':
		return TokenSlash, true
	case '%':
		return TokenPercent, true
	case '@':
		return TokenAt, true
	case '<':
		return TokenLT, true
	case '>':
		return TokenGT, true
	default:
		return "", false
	}
}

func isIdentStart(ch byte) bool {
	return ch == '_' || unicode.IsLetter(rune(ch))
}

func isIdentPart(ch byte) bool {
	return isIdentStart(ch) || isDigit(ch)
}

func isDigit(ch byte) bool {
	return ch >= '0' && ch <= '9'
}
