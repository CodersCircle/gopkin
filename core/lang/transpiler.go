package lang

import (
	"bytes"
	"errors"
	"fmt"
	"go/format"
	"strconv"
	"strings"
	"unicode"
)

// Transpile converts declarative Gopkin (.gpk) source into formatted Pure-Go source.
// If the input already starts with a Go package declaration, it is formatted and
// returned directly for backwards compatibility.
func Transpile(src string) (string, error) {
	trimmed := strings.TrimSpace(src)
	if trimmed == "" {
		return "", errors.New("lang: empty .gpk source")
	}
	if strings.HasPrefix(trimmed, "package ") {
		formatted, err := format.Source([]byte(src))
		if err != nil {
			return "", fmt.Errorf("lang: format raw Go source: %w", err)
		}
		return string(formatted), nil
	}

	tokens, err := tokenize(src)
	if err != nil {
		return "", err
	}

	p := &parser{tokens: tokens}
	prog, err := p.parseProgram()
	if err != nil {
		return "", err
	}

	rawGo, err := generateGo(prog)
	if err != nil {
		return "", err
	}

	formatted, err := format.Source([]byte(rawGo))
	if err != nil {
		return "", fmt.Errorf("lang: format generated Go: %w\n--- generated ---\n%s", err, rawGo)
	}
	return string(formatted), nil
}

// --- Lexer ---

type tokenKind int

const (
	tokEOF tokenKind = iota
	tokIdent
	tokString
	tokNumber
	tokLBrace   // {
	tokRBrace   // }
	tokLParen   // (
	tokRParen   // )
	tokLBracket // [
	tokRBracket // ]
	tokComma    // ,
	tokColon    // :
	tokDot      // .
)

type token struct {
	kind tokenKind
	val  string
	line int
}

func tokenize(src string) ([]token, error) {
	var out []token
	line := 1
	i := 0
	n := len(src)

	for i < n {
		ch := src[i]
		if ch == '\n' {
			line++
			i++
			continue
		}
		if ch == ' ' || ch == '\t' || ch == '\r' {
			i++
			continue
		}
		// Single-line comment // ...
		if ch == '/' && i+1 < n && src[i+1] == '/' {
			for i < n && src[i] != '\n' {
				i++
			}
			continue
		}
		// Hash comment # ...
		if ch == '#' {
			for i < n && src[i] != '\n' {
				i++
			}
			continue
		}

		switch ch {
		case '{':
			out = append(out, token{kind: tokLBrace, val: "{", line: line})
			i++
		case '}':
			out = append(out, token{kind: tokRBrace, val: "}", line: line})
			i++
		case '(':
			out = append(out, token{kind: tokLParen, val: "(", line: line})
			i++
		case ')':
			out = append(out, token{kind: tokRParen, val: ")", line: line})
			i++
		case '[':
			out = append(out, token{kind: tokLBracket, val: "[", line: line})
			i++
		case ']':
			out = append(out, token{kind: tokRBracket, val: "]", line: line})
			i++
		case ',':
			out = append(out, token{kind: tokComma, val: ",", line: line})
			i++
		case ':':
			out = append(out, token{kind: tokColon, val: ":", line: line})
			i++
		case '.':
			out = append(out, token{kind: tokDot, val: ".", line: line})
			i++
		case '"':
			startLine := line
			i++
			var sb strings.Builder
			closed := false
			for i < n {
				c := src[i]
				if c == '\\' && i+1 < n {
					i++
					switch src[i] {
					case 'n':
						sb.WriteByte('\n')
					case 't':
						sb.WriteByte('\t')
					case '"':
						sb.WriteByte('"')
					case '\\':
						sb.WriteByte('\\')
					default:
						sb.WriteByte(src[i])
					}
					i++
					continue
				}
				if c == '"' {
					i++
					closed = true
					break
				}
				if c == '\n' {
					line++
				}
				sb.WriteByte(c)
				i++
			}
			if !closed {
				return nil, fmt.Errorf("lang: line %d: unterminated string literal", startLine)
			}
			out = append(out, token{kind: tokString, val: sb.String(), line: startLine})
		default:
			r := rune(ch)
			if unicode.IsDigit(r) || (ch == '-' && i+1 < n && unicode.IsDigit(rune(src[i+1]))) {
				start := i
				if ch == '-' {
					i++
				}
				for i < n && (unicode.IsDigit(rune(src[i])) || src[i] == '.') {
					i++
				}
				out = append(out, token{kind: tokNumber, val: src[start:i], line: line})
			} else if unicode.IsLetter(r) || ch == '_' {
				start := i
				for i < n && (unicode.IsLetter(rune(src[i])) || unicode.IsDigit(rune(src[i])) || src[i] == '_') {
					i++
				}
				out = append(out, token{kind: tokIdent, val: src[start:i], line: line})
			} else {
				// Skip unrecognized punctuation inside raw handler bodies gracefully
				i++
			}
		}
	}

	out = append(out, token{kind: tokEOF, line: line})
	return out, nil
}

// --- AST ---

type gpkValue struct {
	kind string // "string", "bool", "number", "list", "ident"
	str  string
	bval bool
	list []string
}

type gpkNode struct {
	name     string
	posArgs  []gpkValue
	named    map[string]gpkValue
	children []*gpkNode
}

type gpkProgram struct {
	appTitle string
	dbInit   *gpkNode
	widgets  []*gpkNode
}

// --- Parser ---

type parser struct {
	tokens []token
	pos    int
}

func (p *parser) peek() token {
	if p.pos >= len(p.tokens) {
		return token{kind: tokEOF}
	}
	return p.tokens[p.pos]
}

func (p *parser) peekAt(offset int) token {
	idx := p.pos + offset
	if idx >= len(p.tokens) {
		return token{kind: tokEOF}
	}
	return p.tokens[idx]
}

func (p *parser) next() token {
	t := p.peek()
	if p.pos < len(p.tokens) {
		p.pos++
	}
	return t
}

func (p *parser) parseProgram() (*gpkProgram, error) {
	prog := &gpkProgram{appTitle: "Gopkin App"}

	for p.peek().kind != tokEOF {
		tok := p.peek()
		if tok.kind == tokIdent && tok.val == "app" {
			p.next()
			if p.peek().kind != tokString {
				return nil, fmt.Errorf("lang: line %d: expected app title string after 'app'", tok.line)
			}
			prog.appTitle = p.next().val
			if p.peek().kind != tokLBrace {
				return nil, fmt.Errorf("lang: line %d: expected '{' after app title", p.peek().line)
			}
			p.next()
			for p.peek().kind != tokRBrace && p.peek().kind != tokEOF {
				node, err := p.parseNode()
				if err != nil {
					return nil, err
				}
				if strings.HasPrefix(node.name, "db.") {
					prog.dbInit = node
				} else {
					prog.widgets = append(prog.widgets, node)
				}
			}
			if p.peek().kind != tokRBrace {
				return nil, fmt.Errorf("lang: missing closing '}' for app %q", prog.appTitle)
			}
			p.next()
			continue
		}

		node, err := p.parseNode()
		if err != nil {
			return nil, err
		}
		if strings.HasPrefix(node.name, "db.") {
			prog.dbInit = node
		} else {
			prog.widgets = append(prog.widgets, node)
		}
	}

	return prog, nil
}

func (p *parser) parseNode() (*gpkNode, error) {
	tok := p.next()
	if tok.kind != tokIdent {
		return nil, fmt.Errorf("lang: line %d: expected identifier, got %q", tok.line, tok.val)
	}

	name := tok.val
	for p.peek().kind == tokDot {
		p.next()
		sub := p.next()
		if sub.kind != tokIdent {
			return nil, fmt.Errorf("lang: line %d: expected identifier after '.'", sub.line)
		}
		name += "." + sub.val
	}

	node := &gpkNode{
		name:  name,
		named: make(map[string]gpkValue),
	}

	if p.peek().kind == tokLParen {
		p.next()
		for p.peek().kind != tokRParen && p.peek().kind != tokEOF {
			if p.peek().kind == tokIdent && p.peekAt(1).kind == tokColon {
				key := p.next().val
				p.next() // ':'
				val, err := p.parseValue()
				if err != nil {
					return nil, err
				}
				node.named[key] = val
			} else {
				val, err := p.parseValue()
				if err != nil {
					return nil, err
				}
				node.posArgs = append(node.posArgs, val)
			}
			if p.peek().kind == tokComma {
				p.next()
			}
		}
		if p.peek().kind != tokRParen {
			return nil, fmt.Errorf("lang: line %d: missing ')' for %s", tok.line, name)
		}
		p.next()
	}

	if p.peek().kind == tokLBrace {
		p.next()
		// Action("Delete") { row -> ... } and AuthCard("Sign In") { email, password -> ... }
		// have inline handler blocks; skip opaque body tokens cleanly if not declarative widgets.
		if name == "Action" || name == "AuthCard" {
			depth := 1
			for depth > 0 && p.peek().kind != tokEOF {
				t := p.next()
				if t.kind == tokLBrace {
					depth++
				} else if t.kind == tokRBrace {
					depth--
				}
			}
			return node, nil
		}

		for p.peek().kind != tokRBrace && p.peek().kind != tokEOF {
			child, err := p.parseNode()
			if err != nil {
				return nil, err
			}
			node.children = append(node.children, child)
		}
		if p.peek().kind != tokRBrace {
			return nil, fmt.Errorf("lang: line %d: missing '}' for %s", tok.line, name)
		}
		p.next()
	}

	return node, nil
}

func (p *parser) parseValue() (gpkValue, error) {
	tok := p.next()
	switch tok.kind {
	case tokString:
		return gpkValue{kind: "string", str: tok.val}, nil
	case tokNumber:
		return gpkValue{kind: "number", str: tok.val}, nil
	case tokIdent:
		if tok.val == "true" || tok.val == "false" {
			return gpkValue{kind: "bool", bval: tok.val == "true", str: tok.val}, nil
		}
		return gpkValue{kind: "ident", str: tok.val}, nil
	case tokLBracket:
		var items []string
		for p.peek().kind != tokRBracket && p.peek().kind != tokEOF {
			elem := p.next()
			if elem.kind != tokString && elem.kind != tokIdent && elem.kind != tokNumber {
				return gpkValue{}, fmt.Errorf("lang: line %d: unexpected list item %q", elem.line, elem.val)
			}
			items = append(items, elem.val)
			if p.peek().kind == tokComma {
				p.next()
			}
		}
		if p.peek().kind != tokRBracket {
			return gpkValue{}, fmt.Errorf("lang: line %d: missing ']'", tok.line)
		}
		p.next()
		return gpkValue{kind: "list", list: items}, nil
	default:
		return gpkValue{}, fmt.Errorf("lang: line %d: unexpected token %q in argument", tok.line, tok.val)
	}
}

// --- Pure-Go Code Generator ---

type codeGen struct {
	usesDB bool
}

func generateGo(prog *gpkProgram) (string, error) {
	cg := &codeGen{}
	if prog.dbInit != nil {
		cg.usesDB = true
	}

	var widgetExprs []string
	for _, w := range prog.widgets {
		expr, err := cg.emitWidget(w)
		if err != nil {
			return "", err
		}
		if expr != "" {
			widgetExprs = append(widgetExprs, expr)
		}
	}

	var b bytes.Buffer
	b.WriteString("package main\n\n")
	b.WriteString("import (\n")
	b.WriteString("\t. \"gopkin\"\n")
	if cg.usesDB {
		b.WriteString("\t\"gopkin/backend/db\"\n")
	}
	b.WriteString(")\n\n")
	b.WriteString("func main() {\n")

	if prog.dbInit != nil {
		dbStmt, err := emitDBInit(prog.dbInit)
		if err != nil {
			return "", err
		}
		b.WriteString("\t" + dbStmt + "\n\n")
	}

	b.WriteString(fmt.Sprintf("\t_ = Run(%q", prog.appTitle))
	for _, expr := range widgetExprs {
		b.WriteString(",\n\t\t" + expr)
	}
	b.WriteString(",\n\t)\n")
	b.WriteString("}\n")
	return b.String(), nil
}

func emitDBInit(n *gpkNode) (string, error) {
	switch n.name {
	case "db.Local", "db.LocalSQL":
		path := "app_data.db"
		if len(n.posArgs) > 0 {
			path = n.posArgs[0].str
		}
		return fmt.Sprintf("_ = db.Connect(db.LocalSQL(%q))", path), nil
	case "db.MySQL":
		if len(n.posArgs) == 0 {
			return "", errors.New("lang: db.MySQL requires a DSN string")
		}
		return fmt.Sprintf("_ = db.Connect(db.MySQL(%q))", n.posArgs[0].str), nil
	case "db.MySQLProxy":
		if len(n.posArgs) < 2 {
			return "", errors.New("lang: db.MySQLProxy requires (endpoint, token)")
		}
		return fmt.Sprintf("_ = db.Connect(db.MySQLProxy(%q, %q))", n.posArgs[0].str, n.posArgs[1].str), nil
	default:
		return "", fmt.Errorf("lang: unsupported db directive %q", n.name)
	}
}

func (cg *codeGen) emitWidget(n *gpkNode) (string, error) {
	switch n.name {
	case "Page":
		var parts []string
		if len(n.posArgs) > 0 && n.posArgs[0].str != "" {
			parts = append(parts, fmt.Sprintf("Heading(%q)", n.posArgs[0].str))
		}
		for _, ch := range n.children {
			expr, err := cg.emitWidget(ch)
			if err != nil {
				return "", err
			}
			if expr != "" {
				parts = append(parts, expr)
			}
		}
		return fmt.Sprintf("Column(%s)", strings.Join(parts, ", ")), nil

	case "Card":
		var parts []string
		if len(n.posArgs) > 0 && n.posArgs[0].str != "" {
			parts = append(parts, fmt.Sprintf("Heading(%q)", n.posArgs[0].str))
		}
		for _, ch := range n.children {
			expr, err := cg.emitWidget(ch)
			if err != nil {
				return "", err
			}
			if expr != "" {
				parts = append(parts, expr)
			}
		}
		return fmt.Sprintf("Card(%s)", strings.Join(parts, ", ")), nil

	case "Column", "Row":
		var parts []string
		for _, ch := range n.children {
			expr, err := cg.emitWidget(ch)
			if err != nil {
				return "", err
			}
			if expr != "" {
				parts = append(parts, expr)
			}
		}
		return fmt.Sprintf("%s(%s)", n.name, strings.Join(parts, ", ")), nil

	case "Center":
		if len(n.children) == 0 {
			return "Center(Column())", nil
		}
		inner, err := cg.emitWidget(n.children[0])
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Center(%s)", inner), nil

	case "Heading":
		text := ""
		if len(n.posArgs) > 0 {
			text = n.posArgs[0].str
		}
		return fmt.Sprintf("Heading(%q)", text), nil

	case "Text":
		text := ""
		if len(n.posArgs) > 0 {
			text = n.posArgs[0].str
		}
		return fmt.Sprintf("Text(%q)", text), nil

	case "Badge":
		label := ""
		if len(n.posArgs) > 0 {
			label = n.posArgs[0].str
		}
		variant := "info"
		if v, ok := n.named["variant"]; ok {
			variant = v.str
		} else if len(n.posArgs) > 1 {
			variant = n.posArgs[1].str
		}
		return fmt.Sprintf("Badge(%q, %q)", label, variant), nil

	case "Toast":
		msg := ""
		if len(n.posArgs) > 0 {
			msg = n.posArgs[0].str
		}
		variant := "info"
		if v, ok := n.named["variant"]; ok {
			variant = v.str
		} else if len(n.posArgs) > 1 {
			variant = n.posArgs[1].str
		}
		return fmt.Sprintf("Toast(%q, %q)", msg, variant), nil

	case "Table":
		return cg.emitTable(n)

	case "Form":
		return cg.emitForm(n)

	case "AuthCard":
		title := "Sign In"
		if len(n.posArgs) > 0 && n.posArgs[0].str != "" {
			title = n.posArgs[0].str
		}
		return fmt.Sprintf("AuthCard(%q, func(email, password string) error { return nil })", title), nil

	case "StarterScreen":
		title := "Gopkin App"
		if len(n.posArgs) > 0 && n.posArgs[0].str != "" {
			title = n.posArgs[0].str
		}
		return fmt.Sprintf("StarterScreen(%q)", title), nil

	default:
		return "", fmt.Errorf("lang: unsupported widget %q", n.name)
	}
}

func (cg *codeGen) emitTable(n *gpkNode) (string, error) {
	cg.usesDB = true
	tableName := "items"
	if len(n.posArgs) > 0 && n.posArgs[0].str != "" {
		tableName = n.posArgs[0].str
	} else if t, ok := n.named["table"]; ok && t.str != "" {
		tableName = t.str
	}

	cols := []string{"id", "name"}
	if c, ok := n.named["columns"]; ok && len(c.list) > 0 {
		cols = c.list
	}

	var colExprs []string
	for _, col := range cols {
		colExprs = append(colExprs, fmt.Sprintf("Col(%q, %q)", col, titleCase(col)))
	}

	var chain strings.Builder
	if s, ok := n.named["search"]; ok {
		chain.WriteString(fmt.Sprintf(".Searchable(%t)", s.bval))
	}
	if ps, ok := n.named["pageSize"]; ok {
		if psInt, err := strconv.Atoi(ps.str); err == nil && psInt > 0 {
			chain.WriteString(fmt.Sprintf(".PageSize(%d)", psInt))
		}
	}

	var actionExprs []string
	for _, ch := range n.children {
		if ch.name == "Action" {
			actLabel := "Action"
			if len(ch.posArgs) > 0 && ch.posArgs[0].str != "" {
				actLabel = ch.posArgs[0].str
			}
			if strings.EqualFold(actLabel, "Delete") {
				actionExprs = append(actionExprs, fmt.Sprintf(
					"TableAction(%q, func(row map[string]any) { _, _ = db.Table(%q).Where(\"id\", row[\"id\"]).Delete() })",
					actLabel, tableName,
				))
			} else {
				actionExprs = append(actionExprs, fmt.Sprintf(
					"TableAction(%q, func(row map[string]any) {})", actLabel,
				))
			}
		}
	}
	if len(actionExprs) > 0 {
		chain.WriteString(fmt.Sprintf(".Actions(%s)", strings.Join(actionExprs, ", ")))
	}

	return fmt.Sprintf(
		"func() Widget { rows, _ := db.Table(%q).OrderBy(\"id\", \"DESC\").Get(); return DataTable([]TableColumn{%s}, rows)%s }()",
		tableName,
		strings.Join(colExprs, ", "),
		chain.String(),
	), nil
}

func (cg *codeGen) emitForm(n *gpkNode) (string, error) {
	tableName := ""
	if t, ok := n.named["table"]; ok && t.str != "" {
		tableName = t.str
	} else if len(n.posArgs) > 0 && n.posArgs[0].str != "" {
		tableName = n.posArgs[0].str
	}

	var fieldExprs []string
	submitLabel := ""

	for _, ch := range n.children {
		switch ch.name {
		case "Input", "TextField":
			name := firstString(ch.posArgs, "field")
			label := titleCase(name)
			if v, ok := ch.named["label"]; ok && v.str != "" {
				label = v.str
			}
			expr := fmt.Sprintf("TextField(%q, %q)", name, label)
			if v, ok := ch.named["placeholder"]; ok && v.str != "" {
				expr += fmt.Sprintf(".Placeholder(%q)", v.str)
			}
			if v, ok := ch.named["required"]; ok && v.bval {
				expr += ".Required()"
			}
			if v, ok := ch.named["minLength"]; ok {
				if ml, err := strconv.Atoi(v.str); err == nil && ml > 0 {
					expr += fmt.Sprintf(".MinLength(%d)", ml)
				}
			}
			fieldExprs = append(fieldExprs, expr)

		case "Password", "PasswordField":
			name := firstString(ch.posArgs, "password")
			label := titleCase(name)
			if v, ok := ch.named["label"]; ok && v.str != "" {
				label = v.str
			}
			expr := fmt.Sprintf("PasswordField(%q, %q)", name, label)
			if v, ok := ch.named["required"]; ok && v.bval {
				expr += ".Required()"
			}
			if v, ok := ch.named["minLength"]; ok {
				if ml, err := strconv.Atoi(v.str); err == nil && ml > 0 {
					expr += fmt.Sprintf(".MinLength(%d)", ml)
				}
			}
			fieldExprs = append(fieldExprs, expr)

		case "Select", "SelectField":
			name := firstString(ch.posArgs, "select")
			label := titleCase(name)
			if v, ok := ch.named["label"]; ok && v.str != "" {
				label = v.str
			}
			var opts []string
			if v, ok := ch.named["options"]; ok {
				for _, opt := range v.list {
					opts = append(opts, strconv.Quote(opt))
				}
			}
			if len(opts) > 0 {
				fieldExprs = append(fieldExprs, fmt.Sprintf("SelectField(%q, %q, %s)", name, label, strings.Join(opts, ", ")))
			} else {
				fieldExprs = append(fieldExprs, fmt.Sprintf("SelectField(%q, %q)", name, label))
			}

		case "Toggle", "ToggleField":
			name := firstString(ch.posArgs, "toggle")
			label := titleCase(name)
			if v, ok := ch.named["label"]; ok && v.str != "" {
				label = v.str
			}
			def := false
			if v, ok := ch.named["default"]; ok {
				def = v.bval
			}
			fieldExprs = append(fieldExprs, fmt.Sprintf("ToggleField(%q, %q, %t)", name, label, def))

		case "Submit":
			if len(ch.posArgs) > 0 && ch.posArgs[0].str != "" {
				submitLabel = ch.posArgs[0].str
			}
		}
	}

	expr := fmt.Sprintf("Form(%s)", strings.Join(fieldExprs, ", "))
	if submitLabel != "" {
		expr += fmt.Sprintf(".SubmitLabel(%q)", submitLabel)
	}
	if tableName != "" {
		cg.usesDB = true
		expr += fmt.Sprintf(
			".OnSubmit(func(vals map[string]string) error { data := make(map[string]any, len(vals)); for k, v := range vals { data[k] = v }; _, err := db.Table(%q).Insert(data); return err })",
			tableName,
		)
	}
	return expr, nil
}

func firstString(vals []gpkValue, fallback string) string {
	if len(vals) > 0 && vals[0].str != "" {
		return vals[0].str
	}
	return fallback
}

func titleCase(s string) string {
	if s == "" {
		return ""
	}
	parts := strings.FieldsFunc(s, func(r rune) bool { return r == '_' || r == '-' || r == ' ' })
	for i, p := range parts {
		if len(p) > 0 {
			runes := []rune(p)
			runes[0] = unicode.ToUpper(runes[0])
			parts[i] = string(runes)
		}
	}
	return strings.Join(parts, " ")
}
