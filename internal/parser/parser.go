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
	case lexer.TokenMacro:
		return p.parseMacro(indent)
	case lexer.TokenImport:
		return p.parseImport()
	case lexer.TokenFrom:
		return p.parseFromImport()
	case lexer.TokenIf:
		return p.parseIf(indent)
	case lexer.TokenMatch:
		return p.parseMatch(indent)
	case lexer.TokenWhile:
		return p.parseWhile(indent)
	case lexer.TokenWith:
		return p.parseWith(indent)
	case lexer.TokenTry:
		return p.parseTry(indent)
	case lexer.TokenDefer:
		return p.parseDefer()
	case lexer.TokenAssert:
		return p.parseAssert()
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
	case lexer.TokenCase:
		return nil, fmt.Errorf("line %d: case without matching match", line.Number)
	case lexer.TokenExcept, lexer.TokenFinally:
		return nil, fmt.Errorf("line %d: %s without matching try", line.Number, line.Tokens[0].Lexeme)
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

	if assignIndex := findTopLevelToken(line.Tokens, lexer.TokenAssign); assignIndex >= 0 {
		target, err := parseTargetTokens(line.Tokens[:assignIndex])
		if err != nil {
			return nil, err
		}
		if err := validateTargetExpression(target, line.Number); err != nil {
			return nil, err
		}
		value, err := parseExpressionTokens(line.Tokens[assignIndex+1:])
		if err != nil {
			return nil, err
		}
		return &ast.AssignStmt{
			Line:   line.Number,
			Target: target,
			Value:  value,
		}, nil
	}

	expr, err := parseExpressionTokens(line.Tokens)
	if err != nil {
		return nil, err
	}
	return &ast.ExprStmt{Line: line.Number, Expr: expr}, nil
}

func (p *Parser) parseDefer() (ast.Stmt, error) {
	line := p.lines[p.index]
	if len(line.Tokens) < 2 {
		return nil, fmt.Errorf("line %d: defer requires an expression", line.Number)
	}

	p.index++
	expr, err := parseExpressionTokens(line.Tokens[1:])
	if err != nil {
		return nil, err
	}
	return &ast.DeferStmt{
		Line: line.Number,
		Expr: expr,
	}, nil
}

func (p *Parser) parseAssert() (ast.Stmt, error) {
	line := p.lines[p.index]
	if len(line.Tokens) < 2 {
		return nil, fmt.Errorf("line %d: assert requires a condition", line.Number)
	}

	p.index++
	messageIndex := findTopLevelToken(line.Tokens[1:], lexer.TokenComma)

	conditionTokens := line.Tokens[1:]
	var message ast.Expr
	if messageIndex >= 0 {
		conditionTokens = line.Tokens[1 : 1+messageIndex]
		messageTokens := line.Tokens[1+messageIndex+1:]
		if len(messageTokens) == 0 {
			return nil, fmt.Errorf("line %d: assert message is required after ','", line.Number)
		}
		expr, err := parseExpressionTokens(messageTokens)
		if err != nil {
			return nil, err
		}
		message = expr
	}
	if len(conditionTokens) == 0 {
		return nil, fmt.Errorf("line %d: assert requires a condition", line.Number)
	}

	condition, err := parseExpressionTokens(conditionTokens)
	if err != nil {
		return nil, err
	}
	return &ast.AssertStmt{
		Line:      line.Number,
		Condition: condition,
		Message:   message,
	}, nil
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

func (p *Parser) parseMacro(indent int) (ast.Stmt, error) {
	line := p.lines[p.index]
	cursor := newLineCursor(line.Tokens)

	if _, err := cursor.expect(lexer.TokenMacro); err != nil {
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
		return nil, fmt.Errorf("line %d: macro body is required", line.Number)
	}

	body, err := p.collectRawBody(bodyIndent)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(body) == "" {
		return nil, fmt.Errorf("line %d: macro body is empty", line.Number)
	}

	return &ast.MacroDef{
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

func (p *Parser) parseMatch(indent int) (ast.Stmt, error) {
	line := p.lines[p.index]
	subject, err := parseHeaderExpression(line.Tokens[1:], line.Number)
	if err != nil {
		return nil, err
	}

	p.index++
	caseIndent, ok, err := p.findCodeChildIndent(indent)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("line %d: match block is required", line.Number)
	}

	cases := make([]ast.MatchCase, 0)
	for {
		next, ok := p.peekCodeLine()
		if !ok || next.Indent < caseIndent {
			break
		}
		if next.Indent > caseIndent {
			return nil, fmt.Errorf("line %d: unexpected indentation inside match", next.Number)
		}
		if len(next.Tokens) == 0 || next.Tokens[0].Kind != lexer.TokenCase {
			return nil, fmt.Errorf("line %d: match blocks may only contain case clauses", next.Number)
		}

		matchCase, err := p.parseMatchCase(caseIndent)
		if err != nil {
			return nil, err
		}
		cases = append(cases, matchCase)
	}

	if len(cases) == 0 {
		return nil, fmt.Errorf("line %d: match block must contain at least one case", line.Number)
	}

	return &ast.MatchStmt{
		Line:    line.Number,
		Subject: subject,
		Cases:   cases,
	}, nil
}

func (p *Parser) parseMatchCase(indent int) (ast.MatchCase, error) {
	line := p.lines[p.index]
	cursor := newLineCursor(line.Tokens)
	if _, err := cursor.expect(lexer.TokenCase); err != nil {
		return ast.MatchCase{}, err
	}

	patternTokens, err := cursor.collectUntilTopLevel(lexer.TokenColon)
	if err != nil {
		return ast.MatchCase{}, err
	}
	if len(patternTokens) == 0 {
		return ast.MatchCase{}, fmt.Errorf("line %d: case pattern is required", line.Number)
	}
	guardTokens := []lexer.Token(nil)
	if guardIndex := findTopLevelToken(patternTokens, lexer.TokenIf); guardIndex >= 0 {
		guardTokens = patternTokens[guardIndex+1:]
		patternTokens = patternTokens[:guardIndex]
		if len(patternTokens) == 0 {
			return ast.MatchCase{}, fmt.Errorf("line %d: case pattern is required", line.Number)
		}
		if len(guardTokens) == 0 {
			return ast.MatchCase{}, fmt.Errorf("line %d: case guard is required after if", line.Number)
		}
	}
	pattern, err := parseExpressionTokens(patternTokens)
	if err != nil {
		return ast.MatchCase{}, err
	}
	var guard ast.Expr
	if len(guardTokens) > 0 {
		guard, err = parseExpressionTokens(guardTokens)
		if err != nil {
			return ast.MatchCase{}, err
		}
	}
	if _, err := cursor.expect(lexer.TokenColon); err != nil {
		return ast.MatchCase{}, err
	}
	if !cursor.done() {
		return ast.MatchCase{}, fmt.Errorf("line %d: unexpected token %q", line.Number, cursor.peek().Lexeme)
	}

	p.index++
	bodyIndent, ok, err := p.findCodeChildIndent(indent)
	if err != nil {
		return ast.MatchCase{}, err
	}
	if !ok {
		return ast.MatchCase{}, fmt.Errorf("line %d: case block is required", line.Number)
	}
	body, err := p.parseStatements(bodyIndent)
	if err != nil {
		return ast.MatchCase{}, err
	}

	return ast.MatchCase{
		Line:    line.Number,
		Pattern: pattern,
		Guard:   guard,
		Body:    body,
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

func (p *Parser) parseWith(indent int) (ast.Stmt, error) {
	line := p.lines[p.index]
	cursor := newLineCursor(line.Tokens)
	if _, err := cursor.expect(lexer.TokenWith); err != nil {
		return nil, err
	}

	contextTokens, err := cursor.collectUntilTopLevel(lexer.TokenAs, lexer.TokenColon)
	if err != nil {
		return nil, err
	}
	if len(contextTokens) == 0 {
		return nil, fmt.Errorf("line %d: with requires a context expression", line.Number)
	}
	contextExpr, err := parseExpressionTokens(contextTokens)
	if err != nil {
		return nil, err
	}

	var target ast.Expr
	if cursor.match(lexer.TokenAs) {
		targetTokens, err := cursor.collectUntilTopLevel(lexer.TokenColon)
		if err != nil {
			return nil, err
		}
		if len(targetTokens) == 0 {
			return nil, fmt.Errorf("line %d: with target is required after as", line.Number)
		}
		target, err = parseTargetTokens(targetTokens)
		if err != nil {
			return nil, err
		}
		if err := validateTargetExpression(target, line.Number); err != nil {
			return nil, err
		}
	}
	if _, err := cursor.expect(lexer.TokenColon); err != nil {
		return nil, err
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
		return nil, fmt.Errorf("line %d: with block is required", line.Number)
	}

	body, err := p.parseStatements(bodyIndent)
	if err != nil {
		return nil, err
	}

	return &ast.WithStmt{
		Line:    line.Number,
		Context: contextExpr,
		Target:  target,
		Body:    body,
	}, nil
}

func (p *Parser) parseTry(indent int) (ast.Stmt, error) {
	line := p.lines[p.index]
	if len(line.Tokens) != 2 || line.Tokens[1].Kind != lexer.TokenColon {
		return nil, fmt.Errorf("line %d: try must end with ':'", line.Number)
	}

	p.index++
	bodyIndent, ok, err := p.findCodeChildIndent(indent)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("line %d: try block is required", line.Number)
	}

	body, err := p.parseStatements(bodyIndent)
	if err != nil {
		return nil, err
	}

	var errorName string
	var exceptBody []ast.Stmt
	var finallyBody []ast.Stmt

	next, ok := p.peekCodeLine()
	if ok && next.Indent == indent && len(next.Tokens) > 0 && next.Tokens[0].Kind == lexer.TokenExcept {
		errorName, exceptBody, err = p.parseExcept(indent)
		if err != nil {
			return nil, err
		}
		next, ok = p.peekCodeLine()
	}

	if ok && next.Indent == indent && len(next.Tokens) > 0 && next.Tokens[0].Kind == lexer.TokenFinally {
		finallyBody, err = p.parseFinally(indent)
		if err != nil {
			return nil, err
		}
	}

	if len(exceptBody) == 0 && len(finallyBody) == 0 {
		return nil, fmt.Errorf("line %d: try requires except and/or finally", line.Number)
	}

	return &ast.TryStmt{
		Line:      line.Number,
		Body:      body,
		ErrorName: errorName,
		Except:    exceptBody,
		Finally:   finallyBody,
	}, nil
}

func (p *Parser) parseExcept(indent int) (string, []ast.Stmt, error) {
	line := p.lines[p.index]
	cursor := newLineCursor(line.Tokens)
	if _, err := cursor.expect(lexer.TokenExcept); err != nil {
		return "", nil, err
	}

	errorName := ""
	if !cursor.match(lexer.TokenColon) {
		name, err := cursor.expect(lexer.TokenIdentifier)
		if err != nil {
			return "", nil, err
		}
		errorName = name.Lexeme
		if _, err := cursor.expect(lexer.TokenColon); err != nil {
			return "", nil, err
		}
	}
	if !cursor.done() {
		return "", nil, fmt.Errorf("line %d: unexpected token %q", line.Number, cursor.peek().Lexeme)
	}

	p.index++
	bodyIndent, ok, err := p.findCodeChildIndent(indent)
	if err != nil {
		return "", nil, err
	}
	if !ok {
		return "", nil, fmt.Errorf("line %d: except block is required", line.Number)
	}
	body, err := p.parseStatements(bodyIndent)
	if err != nil {
		return "", nil, err
	}
	return errorName, body, nil
}

func (p *Parser) parseFinally(indent int) ([]ast.Stmt, error) {
	line := p.lines[p.index]
	if len(line.Tokens) != 2 || line.Tokens[1].Kind != lexer.TokenColon {
		return nil, fmt.Errorf("line %d: finally must end with ':'", line.Number)
	}

	p.index++
	bodyIndent, ok, err := p.findCodeChildIndent(indent)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("line %d: finally block is required", line.Number)
	}
	return p.parseStatements(bodyIndent)
}

func (p *Parser) parseFor(indent int) (ast.Stmt, error) {
	line := p.lines[p.index]
	cursor := newLineCursor(line.Tokens)
	if _, err := cursor.expect(lexer.TokenFor); err != nil {
		return nil, err
	}
	targetTokens, err := cursor.collectUntilTopLevel(lexer.TokenIn)
	if err != nil {
		return nil, err
	}
	if len(targetTokens) == 0 {
		return nil, fmt.Errorf("line %d: for target is required", line.Number)
	}
	target, err := parseTargetTokens(targetTokens)
	if err != nil {
		return nil, err
	}
	if err := validateTargetExpression(target, line.Number); err != nil {
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
		Target:   target,
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

func parseTargetTokens(tokens []lexer.Token) (ast.Expr, error) {
	if len(tokens) == 0 {
		return nil, fmt.Errorf("assignment target cannot be empty")
	}

	if parts := splitTopLevelTokens(tokens, lexer.TokenComma); len(parts) > 1 {
		elements := make([]ast.Expr, 0, len(parts))
		for _, part := range parts {
			if len(part) == 0 {
				return nil, fmt.Errorf("assignment target cannot be empty")
			}
			target, err := parseTargetTokens(part)
			if err != nil {
				return nil, err
			}
			elements = append(elements, target)
		}
		return &ast.TargetListExpr{
			Line:     tokens[0].Line,
			Elements: elements,
		}, nil
	}

	if wrapsDelimited(tokens, lexer.TokenLParen, lexer.TokenRParen) {
		return parseTargetTokens(tokens[1 : len(tokens)-1])
	}
	if wrapsDelimited(tokens, lexer.TokenLBracket, lexer.TokenRBracket) {
		inner := tokens[1 : len(tokens)-1]
		if len(inner) == 0 {
			return nil, fmt.Errorf("assignment target cannot be empty")
		}
		parts := splitTopLevelTokens(inner, lexer.TokenComma)
		elements := make([]ast.Expr, 0, len(parts))
		for _, part := range parts {
			if len(part) == 0 {
				return nil, fmt.Errorf("assignment target cannot be empty")
			}
			target, err := parseTargetTokens(part)
			if err != nil {
				return nil, err
			}
			elements = append(elements, target)
		}
		return &ast.TargetListExpr{
			Line:     tokens[0].Line,
			Elements: elements,
		}, nil
	}

	return parseExpressionTokens(tokens)
}

func validateTargetExpression(target ast.Expr, line int) error {
	switch node := target.(type) {
	case *ast.Identifier, *ast.IndexExpr, *ast.MemberExpr:
		return nil
	case *ast.TargetListExpr:
		if len(node.Elements) == 0 {
			return fmt.Errorf("line %d: invalid assignment target", line)
		}
		for _, element := range node.Elements {
			if err := validateTargetExpression(element, line); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("line %d: invalid assignment target", line)
	}
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

func findTopLevelToken(tokens []lexer.Token, kind lexer.TokenKind) int {
	parenDepth := 0
	bracketDepth := 0
	braceDepth := 0

	for index, token := range tokens {
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
		default:
			if parenDepth == 0 && bracketDepth == 0 && braceDepth == 0 && token.Kind == kind {
				return index
			}
		}
	}
	return -1
}

func splitTopLevelTokens(tokens []lexer.Token, delimiter lexer.TokenKind) [][]lexer.Token {
	parenDepth := 0
	bracketDepth := 0
	braceDepth := 0
	parts := make([][]lexer.Token, 0, 1)
	start := 0

	for index, token := range tokens {
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
		if parenDepth == 0 && bracketDepth == 0 && braceDepth == 0 && token.Kind == delimiter {
			parts = append(parts, tokens[start:index])
			start = index + 1
		}
	}
	return append(parts, tokens[start:])
}

func wrapsDelimited(tokens []lexer.Token, left, right lexer.TokenKind) bool {
	if len(tokens) < 2 || tokens[0].Kind != left || tokens[len(tokens)-1].Kind != right {
		return false
	}

	parenDepth := 0
	bracketDepth := 0
	braceDepth := 0
	for index, token := range tokens {
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
		if parenDepth == 0 && bracketDepth == 0 && braceDepth == 0 && index < len(tokens)-1 {
			return false
		}
	}
	return parenDepth == 0 && bracketDepth == 0 && braceDepth == 0
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
	case lexer.TokenAt:
		return p.parseMacroCall(token)
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
		if p.match(lexer.TokenRBracket) {
			return &ast.ListLiteral{Line: token.Line, Elements: []ast.Expr{}}, nil
		}

		first, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		if p.match(lexer.TokenFor) {
			return p.parseListComprehension(token, first)
		}

		items := []ast.Expr{first}
		for p.match(lexer.TokenComma) {
			if p.match(lexer.TokenRBracket) {
				return &ast.ListLiteral{Line: token.Line, Elements: items}, nil
			}
			item, err := p.parseExpression()
			if err != nil {
				return nil, err
			}
			items = append(items, item)
		}
		if !p.match(lexer.TokenRBracket) {
			return nil, p.errorf("expected ']'")
		}
		return &ast.ListLiteral{Line: token.Line, Elements: items}, nil
	case lexer.TokenLBrace:
		if p.match(lexer.TokenRBrace) {
			return &ast.DictLiteral{Line: token.Line, Items: []ast.DictItem{}}, nil
		}

		firstKey, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		if !p.match(lexer.TokenColon) {
			return nil, p.errorf("expected ':' in dictionary literal")
		}
		firstValue, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		if p.match(lexer.TokenFor) {
			return p.parseDictComprehension(token, firstKey, firstValue)
		}

		items := []ast.DictItem{{Key: firstKey, Value: firstValue}}
		for p.match(lexer.TokenComma) {
			if p.match(lexer.TokenRBrace) {
				return &ast.DictLiteral{Line: token.Line, Items: items}, nil
			}
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
		}
		if !p.match(lexer.TokenRBrace) {
			return nil, p.errorf("expected '}'")
		}
		return &ast.DictLiteral{Line: token.Line, Items: items}, nil
	default:
		return nil, p.errorf("unexpected token %q", token.Lexeme)
	}
}

func (p *exprParser) parseListComprehension(token lexer.Token, element ast.Expr) (ast.Expr, error) {
	name, iterable, condition, err := p.parseComprehensionTail()
	if err != nil {
		return nil, err
	}
	if !p.match(lexer.TokenRBracket) {
		return nil, p.errorf("expected ']'")
	}
	return &ast.ListComprehensionExpr{
		Line:      token.Line,
		Element:   element,
		Name:      name,
		Iterable:  iterable,
		Condition: condition,
	}, nil
}

func (p *exprParser) parseDictComprehension(token lexer.Token, key, value ast.Expr) (ast.Expr, error) {
	name, iterable, condition, err := p.parseComprehensionTail()
	if err != nil {
		return nil, err
	}
	if !p.match(lexer.TokenRBrace) {
		return nil, p.errorf("expected '}'")
	}
	return &ast.DictComprehensionExpr{
		Line:      token.Line,
		Key:       key,
		Value:     value,
		Name:      name,
		Iterable:  iterable,
		Condition: condition,
	}, nil
}

func (p *exprParser) parseComprehensionTail() (string, ast.Expr, ast.Expr, error) {
	if !p.check(lexer.TokenIdentifier) {
		return "", nil, nil, p.errorf("expected comprehension variable name")
	}
	name := p.advance().Lexeme
	if !p.match(lexer.TokenIn) {
		return "", nil, nil, p.errorf("expected 'in' in comprehension")
	}
	iterable, err := p.parseExpression()
	if err != nil {
		return "", nil, nil, err
	}
	var condition ast.Expr
	if p.match(lexer.TokenIf) {
		condition, err = p.parseExpression()
		if err != nil {
			return "", nil, nil, err
		}
	}
	return name, iterable, condition, nil
}

func (p *exprParser) parseMacroCall(at lexer.Token) (ast.Expr, error) {
	if p.isAtEnd() {
		return nil, p.errorf("expected macro name after '@'")
	}
	if !p.match(lexer.TokenIdentifier) {
		return nil, p.errorf("expected macro name after '@'")
	}

	callee := ast.Expr(&ast.Identifier{Line: at.Line, Name: p.previous().Lexeme})
	for p.match(lexer.TokenDot) {
		if !p.check(lexer.TokenIdentifier) {
			return nil, p.errorf("expected member name after '.'")
		}
		member := p.advance()
		callee = &ast.MemberExpr{
			Line: at.Line,
			Left: callee,
			Name: member.Lexeme,
		}
	}

	if !p.match(lexer.TokenLParen) {
		return nil, p.errorf("expected '(' after macro name")
	}

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

	return &ast.MacroCallExpr{
		Line:      at.Line,
		Callee:    callee,
		Arguments: args,
	}, nil
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
