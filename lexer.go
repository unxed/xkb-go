package xkb

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// TokenType represents the type of a lexical token in XKB source.
type TokenType int

const (
	TokenEOF TokenType = iota
	TokenError

	// Literals
	TokenIdent   // identifier (xkb_keymap, Shift, AD01)
	TokenString  // "quoted string"
	TokenNumber  // 123, 0x1F, 017
	TokenKeycode // <AD01>

	// Punctuation
	TokenLBrace    // {
	TokenRBrace    // }
	TokenLBracket  // [
	TokenRBracket  // ]
	TokenLParen    // (
	TokenRParen    // )
	TokenSemicolon // ;
	TokenComma     // ,
	TokenEquals    // =
	TokenPlus      // +
	TokenMinus     // -
	TokenBang      // !
	TokenTilde     // ~
	TokenDot       // .
)

var tokenTypeNames = map[TokenType]string{
	TokenEOF:       "EOF",
	TokenError:     "Error",
	TokenIdent:     "Ident",
	TokenString:    "String",
	TokenNumber:    "Number",
	TokenKeycode:   "Keycode",
	TokenLBrace:    "LBrace",
	TokenRBrace:    "RBrace",
	TokenLBracket:  "LBracket",
	TokenRBracket:  "RBracket",
	TokenLParen:    "LParen",
	TokenRParen:    "RParen",
	TokenSemicolon: "Semicolon",
	TokenComma:     "Comma",
	TokenEquals:    "Equals",
	TokenPlus:      "Plus",
	TokenMinus:     "Minus",
	TokenBang:      "Bang",
	TokenTilde:     "Tilde",
	TokenDot:       "Dot",
}

// String returns the string representation of the token type.
func (t TokenType) String() string {
	if name, ok := tokenTypeNames[t]; ok {
		return name
	}
	return fmt.Sprintf("TokenType(%d)", t)
}

// Token represents a lexical token from XKB source.
//
// Returned by [Lexer.NextToken] during parsing.
type Token struct {
	Type  TokenType
	Value string
	Line  int
	Col   int
}

// String returns a string representation of the token for debugging.
func (t Token) String() string {
	if t.Type == TokenEOF {
		return fmt.Sprintf("Token{EOF, line=%d, col=%d}", t.Line, t.Col)
	}
	if t.Type == TokenError {
		return fmt.Sprintf("Token{Error: %q, line=%d, col=%d}", t.Value, t.Line, t.Col)
	}
	return fmt.Sprintf("Token{%s, %q, line=%d, col=%d}", t.Type, t.Value, t.Line, t.Col)
}

// Lexer tokenizes XKB source text into [Token] values.
//
// Create with [NewLexer], then call [Lexer.NextToken] repeatedly.
type Lexer struct {
	input []byte
	pos   int // current position in input
	line  int // current line (1-indexed)
	col   int // current column (1-indexed)
	start int // start position of current token
	width int // width of last rune read
}

// NewLexer creates a new [Lexer] for the given XKB source input.
func NewLexer(input []byte) *Lexer {
	return &Lexer{
		input: input,
		pos:   0,
		line:  1,
		col:   1,
		start: 0,
		width: 0,
	}
}

// next returns the next rune and advances the position.
func (l *Lexer) next() rune {
	if l.pos >= len(l.input) {
		l.width = 0
		return -1 // EOF
	}
	r, w := utf8.DecodeRune(l.input[l.pos:])
	l.width = w
	l.pos += w
	if r == '\n' {
		l.line++
		l.col = 1
	} else {
		l.col++
	}
	return r
}

// backup steps back one rune.
func (l *Lexer) backup() {
	if l.width == 0 {
		return
	}
	l.pos -= l.width
	// Adjust line/col tracking
	if l.pos >= 0 && l.input[l.pos] == '\n' {
		l.line--
		// Recalculate column by scanning back to previous newline
		l.col = 1
		for i := l.pos - 1; i >= 0 && l.input[i] != '\n'; i-- {
			l.col++
		}
	} else {
		l.col--
	}
	l.width = 0
}

// peek returns the next rune without advancing.
func (l *Lexer) peek() rune {
	r := l.next()
	l.backup()
	return r
}

// accept consumes the next rune if it's in the valid set.
func (l *Lexer) accept(valid string) bool {
	if strings.ContainsRune(valid, l.next()) {
		return true
	}
	l.backup()
	return false
}

// acceptRun consumes runes while they're in the valid set.
func (l *Lexer) acceptRun(valid string) {
	for strings.ContainsRune(valid, l.next()) {
	}
	l.backup()
}

// skipWhitespace skips whitespace characters.
func (l *Lexer) skipWhitespace() {
	for {
		r := l.next()
		if r == -1 {
			return
		}
		if !unicode.IsSpace(r) {
			l.backup()
			return
		}
	}
}

// skipComment skips a comment (// or /* */).
func (l *Lexer) skipComment() bool {
	if l.peek() != '/' {
		return false
	}
	l.next() // consume '/'
	r := l.next()
	if r == '/' {
		// Line comment
		for {
			r := l.next()
			if r == -1 || r == '\n' {
				return true
			}
		}
	} else if r == '*' {
		// Block comment
		for {
			r := l.next()
			if r == -1 {
				return true // EOF in comment
			}
			if r == '*' && l.peek() == '/' {
				l.next() // consume '/'
				return true
			}
		}
	}
	// Not a comment, backup
	l.backup()
	l.backup()
	return false
}

// NextToken returns the next token from the input.
func (l *Lexer) NextToken() Token {
	for {
		l.skipWhitespace()
		if l.skipComment() {
			continue
		}
		break
	}

	l.start = l.pos
	startLine := l.line
	startCol := l.col

	r := l.next()
	if r == -1 {
		return Token{Type: TokenEOF, Line: startLine, Col: startCol}
	}

	// Single-character tokens
	switch r {
	case '{':
		return Token{Type: TokenLBrace, Value: "{", Line: startLine, Col: startCol}
	case '}':
		return Token{Type: TokenRBrace, Value: "}", Line: startLine, Col: startCol}
	case '[':
		return Token{Type: TokenLBracket, Value: "[", Line: startLine, Col: startCol}
	case ']':
		return Token{Type: TokenRBracket, Value: "]", Line: startLine, Col: startCol}
	case '(':
		return Token{Type: TokenLParen, Value: "(", Line: startLine, Col: startCol}
	case ')':
		return Token{Type: TokenRParen, Value: ")", Line: startLine, Col: startCol}
	case ';':
		return Token{Type: TokenSemicolon, Value: ";", Line: startLine, Col: startCol}
	case ',':
		return Token{Type: TokenComma, Value: ",", Line: startLine, Col: startCol}
	case '=':
		return Token{Type: TokenEquals, Value: "=", Line: startLine, Col: startCol}
	case '+':
		return Token{Type: TokenPlus, Value: "+", Line: startLine, Col: startCol}
	case '-':
		return Token{Type: TokenMinus, Value: "-", Line: startLine, Col: startCol}
	case '!':
		return Token{Type: TokenBang, Value: "!", Line: startLine, Col: startCol}
	case '~':
		return Token{Type: TokenTilde, Value: "~", Line: startLine, Col: startCol}
	case '.':
		return Token{Type: TokenDot, Value: ".", Line: startLine, Col: startCol}
	case '<':
		return l.scanKeycode(startLine, startCol)
	case '"':
		return l.scanString(startLine, startCol)
	}

	// Numbers
	if unicode.IsDigit(r) {
		l.backup()
		return l.scanNumber(startLine, startCol)
	}

	// Identifiers
	if isIdentStart(r) {
		l.backup()
		return l.scanIdent(startLine, startCol)
	}

	return Token{
		Type:  TokenError,
		Value: fmt.Sprintf("unexpected character: %q", r),
		Line:  startLine,
		Col:   startCol,
	}
}

// scanKeycode scans a keycode token like <AD01>.
func (l *Lexer) scanKeycode(startLine, startCol int) Token {
	var buf strings.Builder
	for {
		r := l.next()
		if r == -1 {
			return Token{
				Type:  TokenError,
				Value: "unterminated keycode",
				Line:  startLine,
				Col:   startCol,
			}
		}
		if r == '>' {
			return Token{
				Type:  TokenKeycode,
				Value: buf.String(),
				Line:  startLine,
				Col:   startCol,
			}
		}
		if !isKeycodeChar(r) {
			return Token{
				Type:  TokenError,
				Value: fmt.Sprintf("invalid character in keycode: %q", r),
				Line:  startLine,
				Col:   startCol,
			}
		}
		buf.WriteRune(r)
	}
}

// scanString scans a quoted string token.
func (l *Lexer) scanString(startLine, startCol int) Token {
	var buf strings.Builder
	for {
		r := l.next()
		if r == -1 || r == '\n' {
			return Token{
				Type:  TokenError,
				Value: "unterminated string",
				Line:  startLine,
				Col:   startCol,
			}
		}
		if r == '"' {
			return Token{
				Type:  TokenString,
				Value: buf.String(),
				Line:  startLine,
				Col:   startCol,
			}
		}
		if r == '\\' {
			// Escape sequence
			escaped := l.next()
			switch escaped {
			case 'n':
				buf.WriteRune('\n')
			case 't':
				buf.WriteRune('\t')
			case 'r':
				buf.WriteRune('\r')
			case '\\':
				buf.WriteRune('\\')
			case '"':
				buf.WriteRune('"')
			case '0', '1', '2', '3', '4', '5', '6', '7':
				// Octal escape
				l.backup()
				val := l.scanOctalEscape()
				buf.WriteRune(rune(val))
			case 'x':
				// Hex escape
				val := l.scanHexEscape(2)
				if val < 0 {
					return Token{
						Type:  TokenError,
						Value: "invalid hex escape",
						Line:  startLine,
						Col:   startCol,
					}
				}
				buf.WriteRune(rune(val))
			default:
				buf.WriteRune(escaped)
			}
		} else {
			buf.WriteRune(r)
		}
	}
}

// scanOctalEscape scans an octal escape sequence (up to 3 digits).
func (l *Lexer) scanOctalEscape() int {
	val := 0
	for i := 0; i < 3; i++ {
		r := l.next()
		if r >= '0' && r <= '7' {
			val = val*8 + int(r-'0')
		} else {
			l.backup()
			break
		}
	}
	return val
}

// scanHexEscape scans a hex escape sequence with the given number of digits.
func (l *Lexer) scanHexEscape(digits int) int {
	val := 0
	for i := 0; i < digits; i++ {
		r := l.next()
		if r >= '0' && r <= '9' {
			val = val*16 + int(r-'0')
		} else if r >= 'a' && r <= 'f' {
			val = val*16 + int(r-'a'+10)
		} else if r >= 'A' && r <= 'F' {
			val = val*16 + int(r-'A'+10)
		} else {
			return -1
		}
	}
	return val
}

// scanNumber scans a numeric token (decimal, hex, or octal).
func (l *Lexer) scanNumber(startLine, startCol int) Token {
	r := l.next()
	if r == '0' {
		// Check for hex or octal
		if l.accept("xX") {
			// Hex number
			l.acceptRun("0123456789abcdefABCDEF")
			return Token{
				Type:  TokenNumber,
				Value: string(l.input[l.start:l.pos]),
				Line:  startLine,
				Col:   startCol,
			}
		}
		// Octal number (or just 0)
		l.acceptRun("01234567")
	}
	// Decimal number
	l.acceptRun("0123456789")
	return Token{
		Type:  TokenNumber,
		Value: string(l.input[l.start:l.pos]),
		Line:  startLine,
		Col:   startCol,
	}
}

// scanIdent scans an identifier token.
func (l *Lexer) scanIdent(startLine, startCol int) Token {
	for {
		r := l.next()
		if !isIdentChar(r) {
			l.backup()
			break
		}
	}
	return Token{
		Type:  TokenIdent,
		Value: string(l.input[l.start:l.pos]),
		Line:  startLine,
		Col:   startCol,
	}
}

// isIdentStart returns true if r can start an identifier.
func isIdentStart(r rune) bool {
	return unicode.IsLetter(r) || r == '_'
}

// isIdentChar returns true if r can be part of an identifier.
func isIdentChar(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
}

// isKeycodeChar returns true if r can be part of a keycode name.
// Keycode names can contain letters, digits, underscores, and +/- (e.g., VOL+, VOL-)
func isKeycodeChar(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '+' || r == '-'
}

// Tokenize returns all tokens from the input.
func (l *Lexer) Tokenize() []Token {
	var tokens []Token
	for {
		tok := l.NextToken()
		tokens = append(tokens, tok)
		if tok.Type == TokenEOF || tok.Type == TokenError {
			break
		}
	}
	return tokens
}
