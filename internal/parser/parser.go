package parser

import (
	"fmt"
	"strconv"
	"strings"

	"vibelang/internal/ast"
	"vibelang/internal/lexer"
)

type Parser struct {
	lines []lexer.Line
	index int
}

func ParseSource(source string) (*ast.Program, error) {
	file, err := lexer.Lex(source)
	if err != nil {
		return nil, err
	}
	return ParseFile(file)
}

func ParseFile(file lexer.File) (*ast.Program, error) {
	parser := &Parser{lines: file.Lines}
	statements, err := parser.parseStatements(0)
	if err != nil {
		return nil, err
	}
	return &ast.Program{Statements: statements}, nil
}

func ParseExpressionSource(source string) (ast.Expr, error) {
	file, err := lexer.Lex(source)
	if err != nil {
		return nil, err
	}

	var tokens []lexer.Token
	found := false
	for _, line := range file.Lines {
		if line.Blank || line.CommentOnly {
			continue
		}
		if line.LexError != nil {
			return nil, line.LexError
		}
		if found {
			return nil, fmt.Errorf("expression source must contain exactly one expression")
		}
		tokens = line.Tokens
		found = true
	}
	if !found {
		return nil, fmt.Errorf("expression source is empty")
	}
	return parseExpressionTokens(tokens)
}

func (p *Parser) parseStatements(indent int) ([]ast.Stmt, error) {
	statements := make([]ast.Stmt, 0)

	for {
		line, ok := p.peekCodeLine()
		if !ok {
			return statements, nil
		}
		if line.Indent < indent {
			return statements, nil
		}
		if line.Indent > indent {
			return nil, fmt.Errorf("line %d: unexpected indentation", line.Number)
		}

		stmt, err := p.parseStatement(indent)
		if err != nil {
			return nil, err
		}
		statements = append(statements, stmt)
	}
}

func (p *Parser) parseStatement(indent int) (ast.Stmt, error) {
	line := p.lines[p.index]
	if line.LexError != nil {
		return nil, line.LexError
	}
	if len(line.Tokens) == 0 {
		return nil, fmt.Errorf("line %d: expected statement", line.Number)
	}

	switch line.Tokens[0].Kind {
	case lexer.TokenDef:
		return p.parseFunction(indent)
	case lexer.TokenImport:
		return p.parseImport()
	case lexer.TokenFrom:
		return p.parseFromImport()
	case lexer.TokenIf:
		return p.parseIf(indent)
	case lexer.TokenWhile:
		return p.parseWhile(indent)
	case lexer.TokenFor:
		return p.parseFor(indent)
	case lexer.TokenBreak:
		p.index++
		if len(line.Tokens) != 1 {
			return nil, fmt.Errorf("line %d: break does not take arguments", line.Number)
		}
		return &ast.BreakStmt{Line: line.Number}, nil
	case lexer.TokenContinue:
		p.index++
		if len(line.Tokens) != 1 {
			return nil, fmt.Errorf("line %d: continue does not take arguments", line.Number)
		}
		return &ast.ContinueStmt{Line: line.Number}, nil
	case lexer.TokenPass:
		p.index++
		if len(line.Tokens) != 1 {
			return nil, fmt.Errorf("line %d: pass does not take arguments", line.Number)
		}
		return &ast.PassStmt{Line: line.Number}, nil
	case lexer.TokenElif, lexer.TokenElse:
		return nil, fmt.Errorf("line %d: %s without matching if", line.Number, line.Tokens[0].Lexeme)
	default:
		return p.parseSimpleStatement()
	}
}

func (p *Parser) parseImport() (ast.Stmt, error) {
	line := p.lines[p.index]
	cursor := newLineCursor(line.Tokens)

	if _, err := cursor.expect(lexer.TokenImport); err != nil {
		return nil, err
	}
	pathToken, err := cursor.expect(lexer.TokenString)
	if err != nil {
		return nil, err
	}
	path, err := strconv.Unquote(pathToken.Lexeme)
	if err != nil {
		return nil, fmt.Errorf("line %d:%d: invalid import path %q", pathToken.Line, pathToken.Column, pathToken.Lexeme)
	}

	alias := ""
	if cursor.match(lexer.TokenAs) {
		name, err := cursor.expect(lexer.TokenIdentifier)
		if err != nil {
			return nil, err
		}
		alias = name.Lexeme
	}
	if !cursor.done() {
		return nil, fmt.Errorf("line %d: unexpected token %q", line.Number, cursor.peek().Lexeme)
	}

	p.index++
	return &ast.ImportStmt{
		Line:  line.Number,
		Path:  path,
		Alias: alias,
	}, nil
}

func (p *Parser) parseFromImport() (ast.Stmt, error) {
	line := p.lines[p.index]
	cursor := newLineCursor(line.Tokens)

	if _, err := cursor.expect(lexer.TokenFrom); err != nil {
		return nil, err
	}
	pathToken, err := cursor.expect(lexer.TokenString)
	if err != nil {
		return nil, err
	}
	path, err := strconv.Unquote(pathToken.Lexeme)
	if err != nil {
		return nil, fmt.Errorf("line %d:%d: invalid import path %q", pathToken.Line, pathToken.Column, pathToken.Lexeme)
	}
	if _, err := cursor.expect(lexer.TokenImport); err != nil {
		return nil, err
	}

	names := make([]ast.ImportName, 0)
	for {
		name, err := cursor.expect(lexer.TokenIdentifier)
		if err != nil {
			return nil, err
		}

		alias := ""
		if cursor.match(lexer.TokenAs) {
			aliasToken, err := cursor.expect(lexer.TokenIdentifier)
			if err != nil {
				return nil, err
			}
			alias = aliasToken.Lexeme
		}

		names = append(names, ast.ImportName{Name: name.Lexeme, Alias: alias})
		if !cursor.match(lexer.TokenComma) {
			break
		}
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("line %d: expected at least one imported name", line.Number)
	}
	if !cursor.done() {
		return nil, fmt.Errorf("line %d: unexpected token %q", line.Number, cursor.peek().Lexeme)
	}

	p.index++
	return &ast.FromImportStmt{
		Line:  line.Number,
		Path:  path,
		Names: names,
	}, nil
}

func (p *Parser) parseSimpleStatement() (ast.Stmt, error) {
	line := p.lines[p.index]
	if line.LexError != nil {
		return nil, line.LexError
	}
	p.index++

	ep := newExprParser(line.Tokens)
	target, err := ep.parseExpression()
	if err != nil {
		return nil, err
	}
	if ep.match(lexer.TokenAssign) {
		value, err := ep.parseExpression()
		if err != nil {
			return nil, err
		}
		if !ep.isAtEnd() {
			return nil, ep.errorf("unexpected token %q", ep.peek().Lexeme)
		}
		switch target.(type) {
		case *ast.Identifier, *ast.IndexExpr, *ast.MemberExpr:
		default:
			return nil, fmt.Errorf("line %d: invalid assignment target", line.Number)
		}
		return &ast.AssignStmt{
			Line:   line.Number,
			Target: target,
			Value:  value,
		}, nil
	}
	if !ep.isAtEnd() {
		return nil, ep.errorf("unexpected token %q", ep.peek().Lexeme)
	}
	return &ast.ExprStmt{Line: line.Number, Expr: target}, nil
}

func (p *Parser) parseFunction(indent int) (ast.Stmt, error) {
	line := p.lines[p.index]
	cursor := newLineCursor(line.Tokens)

	if _, err := cursor.expect(lexer.TokenDef); err != nil {
		return nil, err
	}
	name, err := cursor.expect(lexer.TokenIdentifier)
	if err != nil {
		return nil, err
	}
	if _, err := cursor.expect(lexer.TokenLParen); err != nil {
		return nil, err
	}

	params := make([]ast.Param, 0)
	seenDefault := false
	if !cursor.match(lexer.TokenRParen) {
		for {
			paramTokens, err := cursor.collectUntilTopLevel(lexer.TokenComma, lexer.TokenRParen)
			if err != nil {
				return nil, err
			}
			param, err := parseParamTokens(paramTokens, line.Number)
			if err != nil {
				return nil, err
			}
			if param.Default != nil {
				seenDefault = true
			} else if seenDefault {
				return nil, fmt.Errorf("line %d: parameters without defaults cannot follow parameters with defaults", line.Number)
			}
			params = append(params, param)
			if cursor.match(lexer.TokenComma) {
				if cursor.match(lexer.TokenRParen) {
					break
				}
				continue
			}
			if _, err := cursor.expect(lexer.TokenRParen); err != nil {
				return nil, err
			}
			break
		}
	}

	returnType := ast.TypeRef{}
	if cursor.match(lexer.TokenArrow) {
		typeTokens, err := cursor.collectUntilTopLevel(lexer.TokenColon)
		if err != nil {
			return nil, err
		}
		if len(typeTokens) == 0 {
			return nil, fmt.Errorf("line %d: expected return type", line.Number)
		}
		returnType = ast.TypeRef{Expr: tokensToText(typeTokens)}
	}

	if _, err := cursor.expect(lexer.TokenColon); err != nil {
		return nil, err
	}
	if !cursor.done() {
		return nil, fmt.Errorf("line %d: unexpected token %q", line.Number, cursor.peek().Lexeme)
	}

	p.index++

	bodyIndent, ok, err := p.findRawChildIndent(indent)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("line %d: function body is required", line.Number)
	}

	body, err := p.collectRawBody(bodyIndent)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(body) == "" {
		return nil, fmt.Errorf("line %d: function body is empty", line.Number)
	}

	return &ast.FunctionDef{
		Line:       line.Number,
		Name:       name.Lexeme,
		Params:     params,
		ReturnType: returnType,
		Body:       body,
	}, nil
}

func (p *Parser) parseIf(indent int) (ast.Stmt, error) {
	line := p.lines[p.index]
	condition, err := parseHeaderExpression(line.Tokens[1:], line.Number)
	if err != nil {
		return nil, err
	}

	p.index++
	bodyIndent, ok, err := p.findCodeChildIndent(indent)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("line %d: if block is required", line.Number)
	}

	thenBody, err := p.parseStatements(bodyIndent)
	if err != nil {
		return nil, err
	}

	var elseBody []ast.Stmt

	next, ok := p.peekCodeLine()
	if ok && next.Indent == indent && len(next.Tokens) > 0 {
		switch next.Tokens[0].Kind {
		case lexer.TokenElif:
			elifStmt, err := p.parseElifChain(indent)
			if err != nil {
				return nil, err
			}
			elseBody = []ast.Stmt{elifStmt}
		case lexer.TokenElse:
			p.index++
			if len(next.Tokens) != 2 || next.Tokens[1].Kind != lexer.TokenColon {
				return nil, fmt.Errorf("line %d: else must end with ':'", next.Number)
			}
			elseIndent, ok, err := p.findCodeChildIndent(indent)
			if err != nil {
				return nil, err
			}
			if !ok {
				return nil, fmt.Errorf("line %d: else block is required", next.Number)
			}
			elseBody, err = p.parseStatements(elseIndent)
			if err != nil {
				return nil, err
			}
		}
	}

	return &ast.IfStmt{
		Line:      line.Number,
		Condition: condition,
		Then:      thenBody,
		Else:      elseBody,
	}, nil
}

func (p *Parser) parseElifChain(indent int) (ast.Stmt, error) {
	line := p.lines[p.index]
	condition, err := parseHeaderExpression(line.Tokens[1:], line.Number)
	if err != nil {
		return nil, err
	}

	p.index++
	bodyIndent, ok, err := p.findCodeChildIndent(indent)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("line %d: elif block is required", line.Number)
	}

	thenBody, err := p.parseStatements(bodyIndent)
	if err != nil {
		return nil, err
	}

	var elseBody []ast.Stmt
	next, ok := p.peekCodeLine()
	if ok && next.Indent == indent && len(next.Tokens) > 0 {
		switch next.Tokens[0].Kind {
		case lexer.TokenElif:
			stmt, err := p.parseElifChain(indent)
			if err != nil {
				return nil, err
			}
			elseBody = []ast.Stmt{stmt}
		case lexer.TokenElse:
			p.index++
			if len(next.Tokens) != 2 || next.Tokens[1].Kind != lexer.TokenColon {
				return nil, fmt.Errorf("line %d: else must end with ':'", next.Number)
			}
			elseIndent, ok, err := p.findCodeChildIndent(indent)
			if err != nil {
				return nil, err
			}
			if !ok {
				return nil, fmt.Errorf("line %d: else block is required", next.Number)
			}
			elseBody, err = p.parseStatements(elseIndent)
			if err != nil {
				return nil, err
			}
		}
	}

	return &ast.IfStmt{
		Line:      line.Number,
		Condition: condition,
		Then:      thenBody,
		Else:      elseBody,
	}, nil
}

func (p *Parser) parseWhile(indent int) (ast.Stmt, error) {
	line := p.lines[p.index]
	condition, err := parseHeaderExpression(line.Tokens[1:], line.Number)
	if err != nil {
		return nil, err
	}

	p.index++
	bodyIndent, ok, err := p.findCodeChildIndent(indent)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("line %d: while block is required", line.Number)
	}

	body, err := p.parseStatements(bodyIndent)
	if err != nil {
		return nil, err
	}

	return &ast.WhileStmt{
		Line:      line.Number,
		Condition: condition,
		Body:      body,
	}, nil
}

func (p *Parser) parseFor(indent int) (ast.Stmt, error) {
	line := p.lines[p.index]
	cursor := newLineCursor(line.Tokens)
	if _, err := cursor.expect(lexer.TokenFor); err != nil {
		return nil, err
	}
	name, err := cursor.expect(lexer.TokenIdentifier)
	if err != nil {
		return nil, err
	}
	if _, err := cursor.expect(lexer.TokenIn); err != nil {
		return nil, err
	}

	var iterable ast.Expr
	if cursor.index < len(cursor.tokens) && cursor.tokens[cursor.index].Kind == lexer.TokenPrompt {
		iterable, err = parsePromptToken(cursor.tokens[cursor.index], true)
		if err != nil {
			return nil, err
		}
		cursor.index++
	} else {
		iterTokens, err := cursor.collectUntilTopLevel(lexer.TokenColon)
		if err != nil {
			return nil, err
		}
		if len(iterTokens) == 0 {
			return nil, fmt.Errorf("line %d: expected iterable expression", line.Number)
		}

		iterable, err = parseExpressionTokens(iterTokens)
		if err != nil {
			return nil, err
		}

		if _, err := cursor.expect(lexer.TokenColon); err != nil {
			return nil, err
		}
	}
	if !cursor.done() {
		return nil, fmt.Errorf("line %d: unexpected token %q", line.Number, cursor.peek().Lexeme)
	}

	p.index++
	bodyIndent, ok, err := p.findCodeChildIndent(indent)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("line %d: for block is required", line.Number)
	}

	body, err := p.parseStatements(bodyIndent)
	if err != nil {
		return nil, err
	}

	return &ast.ForStmt{
		Line:     line.Number,
		Name:     name.Lexeme,
		Iterable: iterable,
		Body:     body,
	}, nil
}

func parseHeaderExpression(tokens []lexer.Token, line int) (ast.Expr, error) {
	if len(tokens) == 1 && tokens[0].Kind == lexer.TokenPrompt {
		return parsePromptToken(tokens[0], true)
	}
	if len(tokens) == 0 || tokens[len(tokens)-1].Kind != lexer.TokenColon {
		return nil, fmt.Errorf("line %d: block header must end with ':'", line)
	}
	return parseExpressionTokens(tokens[:len(tokens)-1])
}

func parseExpressionTokens(tokens []lexer.Token) (ast.Expr, error) {
	ep := newExprParser(tokens)
	expr, err := ep.parseExpression()
	if err != nil {
		return nil, err
	}
	if !ep.isAtEnd() {
		return nil, ep.errorf("unexpected token %q", ep.peek().Lexeme)
	}
	return expr, nil
}

func (p *Parser) peekCodeLine() (lexer.Line, bool) {
	for p.index < len(p.lines) {
		line := p.lines[p.index]
		if line.Blank || line.CommentOnly {
			p.index++
			continue
		}
		return line, true
	}
	return lexer.Line{}, false
}

func (p *Parser) findCodeChildIndent(parentIndent int) (int, bool, error) {
	for i := p.index; i < len(p.lines); i++ {
		line := p.lines[i]
		if line.Blank || line.CommentOnly {
			continue
		}
		if line.Indent <= parentIndent {
			return 0, false, nil
		}
		return line.Indent, true, nil
	}
	return 0, false, nil
}

func (p *Parser) findRawChildIndent(parentIndent int) (int, bool, error) {
	for i := p.index; i < len(p.lines); i++ {
		line := p.lines[i]
		if line.Blank {
			continue
		}
		if line.Indent <= parentIndent {
			return 0, false, nil
		}
		return line.Indent, true, nil
	}
	return 0, false, nil
}

func (p *Parser) collectRawBody(bodyIndent int) (string, error) {
	rawLines := make([]string, 0)

	for p.index < len(p.lines) {
		line := p.lines[p.index]
		if !line.Blank && line.Indent < bodyIndent {
			break
		}
		if line.Blank {
			rawLines = append(rawLines, "")
			p.index++
			continue
		}
		if line.Indent < bodyIndent {
			break
		}
		trimmed, err := trimRawIndent(line.Raw, bodyIndent, line.Number)
		if err != nil {
			return "", err
		}
		rawLines = append(rawLines, trimmed)
		p.index++
	}

	return strings.TrimRight(strings.Join(rawLines, "\n"), "\n"), nil
}

func trimRawIndent(raw string, indent, line int) (string, error) {
	if len(raw) < indent {
		return "", fmt.Errorf("line %d: invalid indentation in function body", line)
	}
	for i := 0; i < indent; i++ {
		if raw[i] != ' ' {
			return "", fmt.Errorf("line %d: invalid indentation in function body", line)
		}
	}
	return raw[indent:], nil
}

type lineCursor struct {
	tokens []lexer.Token
	index  int
}

func newLineCursor(tokens []lexer.Token) *lineCursor {
	return &lineCursor{tokens: tokens}
}

func (c *lineCursor) done() bool {
	return c.index >= len(c.tokens)
}

func (c *lineCursor) peek() lexer.Token {
	return c.tokens[c.index]
}

func (c *lineCursor) match(kind lexer.TokenKind) bool {
	if c.done() || c.tokens[c.index].Kind != kind {
		return false
	}
	c.index++
	return true
}

func (c *lineCursor) expect(kind lexer.TokenKind) (lexer.Token, error) {
	if c.done() || c.tokens[c.index].Kind != kind {
		if c.done() {
			return lexer.Token{}, fmt.Errorf("expected %s before end of line", kind)
		}
		token := c.tokens[c.index]
		return lexer.Token{}, fmt.Errorf("line %d:%d: expected %s, found %s", token.Line, token.Column, kind, token.Kind)
	}
	token := c.tokens[c.index]
	c.index++
	return token, nil
}

func (c *lineCursor) collectUntilTopLevel(delimiters ...lexer.TokenKind) ([]lexer.Token, error) {
	collected := make([]lexer.Token, 0)
	parenDepth := 0
	bracketDepth := 0
	braceDepth := 0

	for !c.done() {
		token := c.peek()
		if parenDepth == 0 && bracketDepth == 0 && braceDepth == 0 && tokenKindIn(token.Kind, delimiters) {
			return collected, nil
		}
		switch token.Kind {
		case lexer.TokenLParen:
			parenDepth++
		case lexer.TokenRParen:
			if parenDepth == 0 {
				return nil, fmt.Errorf("line %d:%d: unexpected ')'", token.Line, token.Column)
			}
			parenDepth--
		case lexer.TokenLBracket:
			bracketDepth++
		case lexer.TokenRBracket:
			if bracketDepth == 0 {
				return nil, fmt.Errorf("line %d:%d: unexpected ']'", token.Line, token.Column)
			}
			bracketDepth--
		case lexer.TokenLBrace:
			braceDepth++
		case lexer.TokenRBrace:
			if braceDepth == 0 {
				return nil, fmt.Errorf("line %d:%d: unexpected '}'", token.Line, token.Column)
			}
			braceDepth--
		}
		collected = append(collected, token)
		c.index++
	}

	if len(delimiters) > 0 {
		if len(collected) == 0 {
			first := c.tokens[0]
			return nil, fmt.Errorf("line %d:%d: expected %s", first.Line, first.Column, delimiters[0])
		}
		last := collected[len(collected)-1]
		return nil, fmt.Errorf("line %d:%d: expected %s", last.Line, last.Column, delimiters[0])
	}

	return collected, nil
}

func tokenKindIn(kind lexer.TokenKind, set []lexer.TokenKind) bool {
	for _, candidate := range set {
		if kind == candidate {
			return true
		}
	}
	return false
}

func tokensToText(tokens []lexer.Token) string {
	var builder strings.Builder
	prevIdentLike := false
	for _, token := range tokens {
		identLike := token.Kind == lexer.TokenIdentifier || token.Kind == lexer.TokenNumber ||
			token.Kind == lexer.TokenTrue || token.Kind == lexer.TokenFalse || token.Kind == lexer.TokenNone
		if builder.Len() > 0 && prevIdentLike && identLike {
			builder.WriteByte(' ')
		}
		builder.WriteString(token.Lexeme)
		prevIdentLike = identLike
	}
	return builder.String()
}

func parsePromptToken(token lexer.Token, expectColon bool) (ast.Expr, error) {
	text := strings.TrimSpace(token.Lexeme)
	if expectColon {
		if !strings.HasSuffix(text, ":") {
			return nil, fmt.Errorf("line %d:%d: block header prompt must end with ':'", token.Line, token.Column)
		}
		text = strings.TrimSpace(strings.TrimSuffix(text, ":"))
	}
	if text == "" {
		return nil, fmt.Errorf("line %d:%d: prompt cannot be empty", token.Line, token.Column)
	}
	return &ast.PromptExpr{Line: token.Line, Text: text}, nil
}

func parseParamTokens(tokens []lexer.Token, line int) (ast.Param, error) {
	if len(tokens) == 0 {
		return ast.Param{}, fmt.Errorf("line %d: expected parameter", line)
	}
	if tokens[0].Kind != lexer.TokenIdentifier {
		return ast.Param{}, fmt.Errorf("line %d:%d: expected parameter name", tokens[0].Line, tokens[0].Column)
	}

	param := ast.Param{Name: tokens[0].Lexeme}
	index := 1
	hasDefault := false

	if index < len(tokens) && tokens[index].Kind == lexer.TokenColon {
		index++
		typeTokens, nextIndex, foundDefault := collectTopLevelUntil(tokens, index, lexer.TokenAssign)
		if len(typeTokens) == 0 {
			return ast.Param{}, fmt.Errorf("line %d: expected parameter type", line)
		}
		param.Type = ast.TypeRef{Expr: tokensToText(typeTokens)}
		index = nextIndex
		if foundDefault {
			hasDefault = true
			index++
		}
	} else if index < len(tokens) && tokens[index].Kind == lexer.TokenAssign {
		hasDefault = true
		index++
	}

	if index < len(tokens) {
		if !hasDefault {
			return ast.Param{}, fmt.Errorf("line %d:%d: unexpected token %q", tokens[index].Line, tokens[index].Column, tokens[index].Lexeme)
		}
		defaultExpr, err := parseExpressionTokens(tokens[index:])
		if err != nil {
			return ast.Param{}, err
		}
		param.Default = defaultExpr
		param.DefaultText = tokensToText(tokens[index:])
	}

	return param, nil
}

func collectTopLevelUntil(tokens []lexer.Token, start int, delimiter lexer.TokenKind) ([]lexer.Token, int, bool) {
	collected := make([]lexer.Token, 0)
	parenDepth := 0
	bracketDepth := 0
	braceDepth := 0

	for index := start; index < len(tokens); index++ {
		token := tokens[index]
		if parenDepth == 0 && bracketDepth == 0 && braceDepth == 0 && token.Kind == delimiter {
			return collected, index, true
		}
		switch token.Kind {
		case lexer.TokenLParen:
			parenDepth++
		case lexer.TokenRParen:
			parenDepth--
		case lexer.TokenLBracket:
			bracketDepth++
		case lexer.TokenRBracket:
			bracketDepth--
		case lexer.TokenLBrace:
			braceDepth++
		case lexer.TokenRBrace:
			braceDepth--
		}
		collected = append(collected, token)
	}

	return collected, len(tokens), false
}

type exprParser struct {
	tokens []lexer.Token
	index  int
}

func newExprParser(tokens []lexer.Token) *exprParser {
	return &exprParser{tokens: tokens}
}

func (p *exprParser) parseExpression() (ast.Expr, error) {
	return p.parseOr()
}

func (p *exprParser) parseOr() (ast.Expr, error) {
	expr, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.match(lexer.TokenOr) {
		operator := p.previous()
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		expr = &ast.BinaryExpr{Line: operator.Line, Left: expr, Operator: operator.Lexeme, Right: right}
	}
	return expr, nil
}

func (p *exprParser) parseAnd() (ast.Expr, error) {
	expr, err := p.parseComparison()
	if err != nil {
		return nil, err
	}
	for p.match(lexer.TokenAnd) {
		operator := p.previous()
		right, err := p.parseComparison()
		if err != nil {
			return nil, err
		}
		expr = &ast.BinaryExpr{Line: operator.Line, Left: expr, Operator: operator.Lexeme, Right: right}
	}
	return expr, nil
}

func (p *exprParser) parseComparison() (ast.Expr, error) {
	expr, err := p.parseTerm()
	if err != nil {
		return nil, err
	}
	for p.match(lexer.TokenEq, lexer.TokenNotEq, lexer.TokenLT, lexer.TokenLTE, lexer.TokenGT, lexer.TokenGTE, lexer.TokenIn) {
		operator := p.previous()
		right, err := p.parseTerm()
		if err != nil {
			return nil, err
		}
		expr = &ast.BinaryExpr{Line: operator.Line, Left: expr, Operator: operator.Lexeme, Right: right}
	}
	return expr, nil
}

func (p *exprParser) parseTerm() (ast.Expr, error) {
	expr, err := p.parseFactor()
	if err != nil {
		return nil, err
	}
	for p.match(lexer.TokenPlus, lexer.TokenMinus) {
		operator := p.previous()
		right, err := p.parseFactor()
		if err != nil {
			return nil, err
		}
		expr = &ast.BinaryExpr{Line: operator.Line, Left: expr, Operator: operator.Lexeme, Right: right}
	}
	return expr, nil
}

func (p *exprParser) parseFactor() (ast.Expr, error) {
	expr, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for p.match(lexer.TokenStar, lexer.TokenSlash, lexer.TokenPercent) {
		operator := p.previous()
		right, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		expr = &ast.BinaryExpr{Line: operator.Line, Left: expr, Operator: operator.Lexeme, Right: right}
	}
	return expr, nil
}

func (p *exprParser) parseUnary() (ast.Expr, error) {
	if p.match(lexer.TokenMinus, lexer.TokenNot) {
		operator := p.previous()
		right, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &ast.UnaryExpr{Line: operator.Line, Operator: operator.Lexeme, Right: right}, nil
	}
	return p.parsePostfix()
}

func (p *exprParser) parsePostfix() (ast.Expr, error) {
	expr, err := p.parsePrimary()
	if err != nil {
		return nil, err
	}

	for {
		if p.match(lexer.TokenLParen) {
			args := make([]ast.CallArgument, 0)
			seenKeyword := false
			if !p.match(lexer.TokenRParen) {
				for {
					arg, err := p.parseCallArgument()
					if err != nil {
						return nil, err
					}
					if arg.Name != "" {
						seenKeyword = true
					} else if seenKeyword {
						return nil, p.errorf("positional arguments cannot follow keyword arguments")
					}
					args = append(args, arg)
					if p.match(lexer.TokenComma) {
						if p.match(lexer.TokenRParen) {
							break
						}
						continue
					}
					if !p.match(lexer.TokenRParen) {
						return nil, p.errorf("expected ')'")
					}
					break
				}
			}
			expr = &ast.CallExpr{Line: expr.LineNumber(), Callee: expr, Arguments: args}
			continue
		}

		if p.match(lexer.TokenLBracket) {
			accessExpr, err := p.parseBracketAccess(expr)
			if err != nil {
				return nil, err
			}
			expr = accessExpr
			continue
		}

		if p.match(lexer.TokenDot) {
			if !p.check(lexer.TokenIdentifier) {
				return nil, p.errorf("expected member name after '.'")
			}
			member := p.advance()
			expr = &ast.MemberExpr{
				Line: expr.LineNumber(),
				Left: expr,
				Name: member.Lexeme,
			}
			continue
		}

		return expr, nil
	}
}

func (p *exprParser) parseBracketAccess(left ast.Expr) (ast.Expr, error) {
	content, err := p.collectBracketContent()
	if err != nil {
		return nil, err
	}

	firstColon := topLevelTokenIndex(content, lexer.TokenColon)
	if firstColon < 0 {
		indexExpr, err := parseExpressionTokens(content)
		if err != nil {
			return nil, err
		}
		return &ast.IndexExpr{Line: left.LineNumber(), Left: left, Index: indexExpr}, nil
	}

	startExpr, err := parseOptionalExpressionTokens(content[:firstColon])
	if err != nil {
		return nil, err
	}

	remaining := content[firstColon+1:]
	secondColon := topLevelTokenIndex(remaining, lexer.TokenColon)

	var endTokens []lexer.Token
	var stepTokens []lexer.Token
	if secondColon >= 0 {
		endTokens = remaining[:secondColon]
		stepTokens = remaining[secondColon+1:]
	} else {
		endTokens = remaining
	}

	endExpr, err := parseOptionalExpressionTokens(endTokens)
	if err != nil {
		return nil, err
	}
	stepExpr, err := parseOptionalExpressionTokens(stepTokens)
	if err != nil {
		return nil, err
	}

	return &ast.SliceExpr{
		Line:  left.LineNumber(),
		Left:  left,
		Start: startExpr,
		End:   endExpr,
		Step:  stepExpr,
	}, nil
}

func (p *exprParser) collectBracketContent() ([]lexer.Token, error) {
	collected := make([]lexer.Token, 0)
	parenDepth := 0
	bracketDepth := 0
	braceDepth := 0

	for !p.isAtEnd() {
		token := p.peek()
		if token.Kind == lexer.TokenRBracket && parenDepth == 0 && bracketDepth == 0 && braceDepth == 0 {
			p.advance()
			return collected, nil
		}

		switch token.Kind {
		case lexer.TokenLParen:
			parenDepth++
		case lexer.TokenRParen:
			if parenDepth == 0 {
				return nil, p.errorf("unexpected ')'")
			}
			parenDepth--
		case lexer.TokenLBracket:
			bracketDepth++
		case lexer.TokenRBracket:
			if bracketDepth == 0 {
				return nil, p.errorf("unexpected ']'")
			}
			bracketDepth--
		case lexer.TokenLBrace:
			braceDepth++
		case lexer.TokenRBrace:
			if braceDepth == 0 {
				return nil, p.errorf("unexpected '}'")
			}
			braceDepth--
		}

		collected = append(collected, token)
		p.advance()
	}

	return nil, p.errorf("expected ']'")
}

func parseOptionalExpressionTokens(tokens []lexer.Token) (ast.Expr, error) {
	if len(tokens) == 0 {
		return nil, nil
	}
	return parseExpressionTokens(tokens)
}

func topLevelTokenIndex(tokens []lexer.Token, delimiter lexer.TokenKind) int {
	parenDepth := 0
	bracketDepth := 0
	braceDepth := 0

	for index, token := range tokens {
		if parenDepth == 0 && bracketDepth == 0 && braceDepth == 0 && token.Kind == delimiter {
			return index
		}

		switch token.Kind {
		case lexer.TokenLParen:
			parenDepth++
		case lexer.TokenRParen:
			parenDepth--
		case lexer.TokenLBracket:
			bracketDepth++
		case lexer.TokenRBracket:
			bracketDepth--
		case lexer.TokenLBrace:
			braceDepth++
		case lexer.TokenRBrace:
			braceDepth--
		}
	}

	return -1
}

func (p *exprParser) parseCallArgument() (ast.CallArgument, error) {
	if p.check(lexer.TokenIdentifier) && p.checkNext(lexer.TokenAssign) {
		name := p.advance()
		p.advance()
		value, err := p.parseExpression()
		if err != nil {
			return ast.CallArgument{}, err
		}
		return ast.CallArgument{Name: name.Lexeme, Value: value}, nil
	}

	value, err := p.parseExpression()
	if err != nil {
		return ast.CallArgument{}, err
	}
	return ast.CallArgument{Value: value}, nil
}

func (p *exprParser) parsePrimary() (ast.Expr, error) {
	if p.isAtEnd() {
		return nil, p.errorf("expected expression")
	}

	token := p.advance()
	switch token.Kind {
	case lexer.TokenIdentifier:
		return &ast.Identifier{Line: token.Line, Name: token.Lexeme}, nil
	case lexer.TokenNumber:
		if strings.Contains(token.Lexeme, ".") {
			value, err := strconv.ParseFloat(token.Lexeme, 64)
			if err != nil {
				return nil, p.errorf("invalid float literal %q", token.Lexeme)
			}
			return &ast.Literal{Line: token.Line, Value: value}, nil
		}
		value, err := strconv.ParseInt(token.Lexeme, 10, 64)
		if err != nil {
			return nil, p.errorf("invalid integer literal %q", token.Lexeme)
		}
		return &ast.Literal{Line: token.Line, Value: value}, nil
	case lexer.TokenString:
		value, err := strconv.Unquote(token.Lexeme)
		if err != nil {
			return nil, p.errorf("invalid string literal %q", token.Lexeme)
		}
		return &ast.Literal{Line: token.Line, Value: value}, nil
	case lexer.TokenPrompt:
		return parsePromptToken(token, false)
	case lexer.TokenTrue:
		return &ast.Literal{Line: token.Line, Value: true}, nil
	case lexer.TokenFalse:
		return &ast.Literal{Line: token.Line, Value: false}, nil
	case lexer.TokenNone:
		return &ast.Literal{Line: token.Line, Value: nil}, nil
	case lexer.TokenLParen:
		expr, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		if !p.match(lexer.TokenRParen) {
			return nil, p.errorf("expected ')'")
		}
		return expr, nil
	case lexer.TokenLBracket:
		items := make([]ast.Expr, 0)
		if !p.match(lexer.TokenRBracket) {
			for {
				item, err := p.parseExpression()
				if err != nil {
					return nil, err
				}
				items = append(items, item)
				if p.match(lexer.TokenComma) {
					continue
				}
				if !p.match(lexer.TokenRBracket) {
					return nil, p.errorf("expected ']'")
				}
				break
			}
		}
		return &ast.ListLiteral{Line: token.Line, Elements: items}, nil
	case lexer.TokenLBrace:
		items := make([]ast.DictItem, 0)
		if !p.match(lexer.TokenRBrace) {
			for {
				key, err := p.parseExpression()
				if err != nil {
					return nil, err
				}
				if !p.match(lexer.TokenColon) {
					return nil, p.errorf("expected ':' in dictionary literal")
				}
				value, err := p.parseExpression()
				if err != nil {
					return nil, err
				}
				items = append(items, ast.DictItem{Key: key, Value: value})
				if p.match(lexer.TokenComma) {
					continue
				}
				if !p.match(lexer.TokenRBrace) {
					return nil, p.errorf("expected '}'")
				}
				break
			}
		}
		return &ast.DictLiteral{Line: token.Line, Items: items}, nil
	default:
		return nil, p.errorf("unexpected token %q", token.Lexeme)
	}
}

func (p *exprParser) match(kinds ...lexer.TokenKind) bool {
	if p.isAtEnd() {
		return false
	}
	for _, kind := range kinds {
		if p.tokens[p.index].Kind == kind {
			p.index++
			return true
		}
	}
	return false
}

func (p *exprParser) check(kind lexer.TokenKind) bool {
	if p.isAtEnd() {
		return false
	}
	return p.tokens[p.index].Kind == kind
}

func (p *exprParser) checkNext(kind lexer.TokenKind) bool {
	if p.index+1 >= len(p.tokens) {
		return false
	}
	return p.tokens[p.index+1].Kind == kind
}

func (p *exprParser) advance() lexer.Token {
	token := p.tokens[p.index]
	p.index++
	return token
}

func (p *exprParser) previous() lexer.Token {
	return p.tokens[p.index-1]
}

func (p *exprParser) peek() lexer.Token {
	return p.tokens[p.index]
}

func (p *exprParser) isAtEnd() bool {
	return p.index >= len(p.tokens)
}

func (p *exprParser) errorf(format string, args ...any) error {
	if p.isAtEnd() {
		return fmt.Errorf(format, args...)
	}
	token := p.peek()
	return fmt.Errorf("line %d:%d: %s", token.Line, token.Column, fmt.Sprintf(format, args...))
}
