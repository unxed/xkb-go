package xkb

import (
	"strings"
	"testing"
)

func TestLexerPunctuation(t *testing.T) {
	tests := []struct {
		input string
		want  TokenType
	}{
		{"{", TokenLBrace},
		{"}", TokenRBrace},
		{"[", TokenLBracket},
		{"]", TokenRBracket},
		{"(", TokenLParen},
		{")", TokenRParen},
		{";", TokenSemicolon},
		{",", TokenComma},
		{"=", TokenEquals},
		{"+", TokenPlus},
		{"-", TokenMinus},
		{"!", TokenBang},
		{"~", TokenTilde},
		{".", TokenDot},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			lexer := NewLexer([]byte(tt.input))
			tok := lexer.NextToken()
			if tok.Type != tt.want {
				t.Errorf("got %v, want %v", tok.Type, tt.want)
			}
			if tok.Value != tt.input {
				t.Errorf("value = %q, want %q", tok.Value, tt.input)
			}
		})
	}
}

func TestLexerIdentifiers(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"xkb_keymap", "xkb_keymap"},
		{"Shift", "Shift"},
		{"Level1", "Level1"},
		{"AD01", "AD01"},
		{"_private", "_private"},
		{"modifier_map", "modifier_map"},
		{"ALPHABETIC", "ALPHABETIC"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			lexer := NewLexer([]byte(tt.input))
			tok := lexer.NextToken()
			if tok.Type != TokenIdent {
				t.Errorf("type = %v, want TokenIdent", tok.Type)
			}
			if tok.Value != tt.want {
				t.Errorf("value = %q, want %q", tok.Value, tt.want)
			}
		})
	}
}

func TestLexerStrings(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"simple", `"evdev"`, "evdev"},
		{"with spaces", `"English (US)"`, "English (US)"},
		{"escape newline", `"line1\nline2"`, "line1\nline2"},
		{"escape tab", `"col1\tcol2"`, "col1\tcol2"},
		{"escape quote", `"say \"hello\""`, `say "hello"`},
		{"escape backslash", `"path\\file"`, `path\file`},
		{"empty", `""`, ""},
		{"octal escape", `"\101"`, "A"}, // 0101 = 65 = 'A'
		{"hex escape", `"\x41"`, "A"},   // 0x41 = 65 = 'A'
		{"escape carriage return", `"a\rb"`, "a\rb"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lexer := NewLexer([]byte(tt.input))
			tok := lexer.NextToken()
			if tok.Type != TokenString {
				t.Errorf("type = %v, want TokenString", tok.Type)
			}
			if tok.Value != tt.want {
				t.Errorf("value = %q, want %q", tok.Value, tt.want)
			}
		})
	}
}

func TestLexerStringErrors(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"unterminated", `"hello`},
		{"newline in string", "\"hello\nworld\""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lexer := NewLexer([]byte(tt.input))
			tok := lexer.NextToken()
			if tok.Type != TokenError {
				t.Errorf("type = %v, want TokenError", tok.Type)
			}
		})
	}
}

func TestLexerNumbers(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"0", "0"},
		{"123", "123"},
		{"255", "255"},
		{"0x1F", "0x1F"},
		{"0xFF", "0xFF"},
		{"0xABCD", "0xABCD"},
		{"017", "017"},   // octal
		{"0755", "0755"}, // octal
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			lexer := NewLexer([]byte(tt.input))
			tok := lexer.NextToken()
			if tok.Type != TokenNumber {
				t.Errorf("type = %v, want TokenNumber", tok.Type)
			}
			if tok.Value != tt.want {
				t.Errorf("value = %q, want %q", tok.Value, tt.want)
			}
		})
	}
}

func TestLexerKeycodes(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"<AD01>", "AD01"},
		{"<ESC>", "ESC"},
		{"<AE01>", "AE01"},
		{"<LFSH>", "LFSH"},
		{"<RALT>", "RALT"},
		{"<FK01>", "FK01"},
		{"<I120>", "I120"},
		{"<AB_C>", "AB_C"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			lexer := NewLexer([]byte(tt.input))
			tok := lexer.NextToken()
			if tok.Type != TokenKeycode {
				t.Errorf("type = %v, want TokenKeycode", tok.Type)
			}
			if tok.Value != tt.want {
				t.Errorf("value = %q, want %q", tok.Value, tt.want)
			}
		})
	}
}

func TestLexerKeycodeErrors(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"unterminated", "<AD01"},
		{"invalid char", "<AD$01>"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lexer := NewLexer([]byte(tt.input))
			tok := lexer.NextToken()
			if tok.Type != TokenError {
				t.Errorf("type = %v, want TokenError", tok.Type)
			}
		})
	}
}

func TestLexerComments(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			"line comment",
			"foo // this is a comment\nbar",
			"foo",
		},
		{
			"block comment",
			"foo /* block */ bar",
			"foo",
		},
		{
			"block comment multiline",
			"foo /* line1\nline2 */ bar",
			"foo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lexer := NewLexer([]byte(tt.input))
			tok := lexer.NextToken()
			if tok.Type != TokenIdent {
				t.Errorf("type = %v, want TokenIdent", tok.Type)
			}
			if tok.Value != tt.want {
				t.Errorf("value = %q, want %q", tok.Value, tt.want)
			}
		})
	}
}

func TestLexerWhitespace(t *testing.T) {
	input := "   \t\n   foo   \n   bar   "
	lexer := NewLexer([]byte(input))

	tok1 := lexer.NextToken()
	if tok1.Type != TokenIdent || tok1.Value != "foo" {
		t.Errorf("first token = %v, want Ident(foo)", tok1)
	}

	tok2 := lexer.NextToken()
	if tok2.Type != TokenIdent || tok2.Value != "bar" {
		t.Errorf("second token = %v, want Ident(bar)", tok2)
	}

	tok3 := lexer.NextToken()
	if tok3.Type != TokenEOF {
		t.Errorf("third token = %v, want EOF", tok3)
	}
}

func TestLexerLineColumn(t *testing.T) {
	input := "foo\nbar\n  baz"
	lexer := NewLexer([]byte(input))

	tok1 := lexer.NextToken()
	if tok1.Line != 1 || tok1.Col != 1 {
		t.Errorf("foo position = (%d,%d), want (1,1)", tok1.Line, tok1.Col)
	}

	tok2 := lexer.NextToken()
	if tok2.Line != 2 || tok2.Col != 1 {
		t.Errorf("bar position = (%d,%d), want (2,1)", tok2.Line, tok2.Col)
	}

	tok3 := lexer.NextToken()
	if tok3.Line != 3 || tok3.Col != 3 {
		t.Errorf("baz position = (%d,%d), want (3,3)", tok3.Line, tok3.Col)
	}
}

func TestLexerEOF(t *testing.T) {
	lexer := NewLexer([]byte(""))
	tok := lexer.NextToken()
	if tok.Type != TokenEOF {
		t.Errorf("type = %v, want TokenEOF", tok.Type)
	}
}

func TestLexerUnexpectedChar(t *testing.T) {
	lexer := NewLexer([]byte("@"))
	tok := lexer.NextToken()
	if tok.Type != TokenError {
		t.Errorf("type = %v, want TokenError", tok.Type)
	}
}

func TestLexerTokenize(t *testing.T) {
	input := `xkb_keymap {
		xkb_keycodes "test" {
			<AD01> = 24;
		};
	};`

	lexer := NewLexer([]byte(input))
	tokens := lexer.Tokenize()

	expected := []struct {
		typ   TokenType
		value string
	}{
		{TokenIdent, "xkb_keymap"},
		{TokenLBrace, "{"},
		{TokenIdent, "xkb_keycodes"},
		{TokenString, "test"},
		{TokenLBrace, "{"},
		{TokenKeycode, "AD01"},
		{TokenEquals, "="},
		{TokenNumber, "24"},
		{TokenSemicolon, ";"},
		{TokenRBrace, "}"},
		{TokenSemicolon, ";"},
		{TokenRBrace, "}"},
		{TokenSemicolon, ";"},
		{TokenEOF, ""},
	}

	if len(tokens) != len(expected) {
		t.Fatalf("got %d tokens, want %d", len(tokens), len(expected))
	}

	for i, exp := range expected {
		if tokens[i].Type != exp.typ {
			t.Errorf("token[%d].Type = %v, want %v", i, tokens[i].Type, exp.typ)
		}
		if tokens[i].Value != exp.value {
			t.Errorf("token[%d].Value = %q, want %q", i, tokens[i].Value, exp.value)
		}
	}
}

func TestLexerComplexInput(t *testing.T) {
	// A more realistic XKB snippet
	input := `xkb_types "complete" {
		virtual_modifiers NumLock, Alt;

		type "ALPHABETIC" {
			modifiers = Shift + Lock;
			map[Shift] = Level2;
			level_name[Level1] = "Base";
		};
	};`

	lexer := NewLexer([]byte(input))
	tokens := lexer.Tokenize()

	// Just verify it tokenizes without error
	lastToken := tokens[len(tokens)-1]
	if lastToken.Type == TokenError {
		t.Errorf("tokenization failed: %s", lastToken.Value)
	}
	if lastToken.Type != TokenEOF {
		t.Errorf("last token = %v, want EOF", lastToken)
	}
}

func TestLexerKeyDefinition(t *testing.T) {
	input := `key <AD01> { [ q, Q ] };`
	lexer := NewLexer([]byte(input))
	tokens := lexer.Tokenize()

	expected := []TokenType{
		TokenIdent,     // key
		TokenKeycode,   // AD01
		TokenLBrace,    // {
		TokenLBracket,  // [
		TokenIdent,     // q
		TokenComma,     // ,
		TokenIdent,     // Q
		TokenRBracket,  // ]
		TokenRBrace,    // }
		TokenSemicolon, // ;
		TokenEOF,
	}

	if len(tokens) != len(expected) {
		t.Fatalf("got %d tokens, want %d", len(tokens), len(expected))
	}

	for i, exp := range expected {
		if tokens[i].Type != exp {
			t.Errorf("token[%d].Type = %v, want %v", i, tokens[i].Type, exp)
		}
	}
}

func TestLexerActionSyntax(t *testing.T) {
	input := `SetMods(modifiers=Shift)`
	lexer := NewLexer([]byte(input))
	tokens := lexer.Tokenize()

	expected := []struct {
		typ   TokenType
		value string
	}{
		{TokenIdent, "SetMods"},
		{TokenLParen, "("},
		{TokenIdent, "modifiers"},
		{TokenEquals, "="},
		{TokenIdent, "Shift"},
		{TokenRParen, ")"},
		{TokenEOF, ""},
	}

	if len(tokens) != len(expected) {
		t.Fatalf("got %d tokens, want %d", len(tokens), len(expected))
	}

	for i, exp := range expected {
		if tokens[i].Type != exp.typ || tokens[i].Value != exp.value {
			t.Errorf("token[%d] = %v, want {%v, %q}", i, tokens[i], exp.typ, exp.value)
		}
	}
}

func TestLexerModifierExpression(t *testing.T) {
	input := `modifiers = Shift + Lock;`
	lexer := NewLexer([]byte(input))
	tokens := lexer.Tokenize()

	expected := []TokenType{
		TokenIdent,     // modifiers
		TokenEquals,    // =
		TokenIdent,     // Shift
		TokenPlus,      // +
		TokenIdent,     // Lock
		TokenSemicolon, // ;
		TokenEOF,
	}

	if len(tokens) != len(expected) {
		t.Fatalf("got %d tokens, want %d", len(tokens), len(expected))
	}

	for i, exp := range expected {
		if tokens[i].Type != exp {
			t.Errorf("token[%d].Type = %v, want %v", i, tokens[i].Type, exp)
		}
	}
}

func TestLexerNegation(t *testing.T) {
	input := `!Shift`
	lexer := NewLexer([]byte(input))
	tokens := lexer.Tokenize()

	if len(tokens) != 3 { // !, Shift, EOF
		t.Fatalf("got %d tokens, want 3", len(tokens))
	}
	if tokens[0].Type != TokenBang {
		t.Errorf("token[0].Type = %v, want TokenBang", tokens[0].Type)
	}
	if tokens[1].Type != TokenIdent || tokens[1].Value != "Shift" {
		t.Errorf("token[1] = %v, want Ident(Shift)", tokens[1])
	}
}

func TestLexerTildeModifier(t *testing.T) {
	input := `~LevelThree`
	lexer := NewLexer([]byte(input))
	tokens := lexer.Tokenize()

	if len(tokens) != 3 { // ~, LevelThree, EOF
		t.Fatalf("got %d tokens, want 3", len(tokens))
	}
	if tokens[0].Type != TokenTilde {
		t.Errorf("token[0].Type = %v, want TokenTilde", tokens[0].Type)
	}
}

func TestTokenTypeString(t *testing.T) {
	tests := []struct {
		typ  TokenType
		want string
	}{
		{TokenEOF, "EOF"},
		{TokenError, "Error"},
		{TokenIdent, "Ident"},
		{TokenString, "String"},
		{TokenNumber, "Number"},
		{TokenKeycode, "Keycode"},
		{TokenLBrace, "LBrace"},
		{TokenType(999), "TokenType(999)"},
	}

	for _, tt := range tests {
		got := tt.typ.String()
		if got != tt.want {
			t.Errorf("TokenType(%d).String() = %q, want %q", tt.typ, got, tt.want)
		}
	}
}

func TestTokenString(t *testing.T) {
	tests := []struct {
		tok  Token
		want string
	}{
		{
			Token{Type: TokenEOF, Line: 1, Col: 1},
			"Token{EOF, line=1, col=1}",
		},
		{
			Token{Type: TokenError, Value: "bad input", Line: 5, Col: 10},
			`Token{Error: "bad input", line=5, col=10}`,
		},
		{
			Token{Type: TokenIdent, Value: "foo", Line: 2, Col: 3},
			`Token{Ident, "foo", line=2, col=3}`,
		},
	}

	for _, tt := range tests {
		got := tt.tok.String()
		if got != tt.want {
			t.Errorf("Token.String() = %q, want %q", got, tt.want)
		}
	}
}

func TestLexerHexEscapeError(t *testing.T) {
	// Invalid hex escape (not enough digits)
	input := `"\xG"`
	lexer := NewLexer([]byte(input))
	tok := lexer.NextToken()
	if tok.Type != TokenError {
		t.Errorf("type = %v, want TokenError", tok.Type)
	}
}

func TestLexerPeek(t *testing.T) {
	lexer := NewLexer([]byte("ab"))

	// Peek should not advance
	r := lexer.peek()
	if r != 'a' {
		t.Errorf("peek() = %q, want 'a'", r)
	}

	// Peek again should return same
	r = lexer.peek()
	if r != 'a' {
		t.Errorf("peek() again = %q, want 'a'", r)
	}

	// Next should now return 'a'
	r = lexer.next()
	if r != 'a' {
		t.Errorf("next() = %q, want 'a'", r)
	}

	// Next should now return 'b'
	r = lexer.next()
	if r != 'b' {
		t.Errorf("next() = %q, want 'b'", r)
	}
}

func TestLexerBackupAtNewline(t *testing.T) {
	input := "a\nb"
	lexer := NewLexer([]byte(input))

	// Read 'a'
	r := lexer.next()
	if r != 'a' {
		t.Errorf("next() = %q, want 'a'", r)
	}

	// Read '\n'
	r = lexer.next()
	if r != '\n' {
		t.Errorf("next() = %q, want '\\n'", r)
	}
	if lexer.line != 2 {
		t.Errorf("line = %d, want 2", lexer.line)
	}

	// Backup should restore line
	lexer.backup()
	if lexer.line != 1 {
		t.Errorf("after backup: line = %d, want 1", lexer.line)
	}
}

func TestLexerIncludeStatement(t *testing.T) {
	input := `include "us(basic)"`
	lexer := NewLexer([]byte(input))
	tokens := lexer.Tokenize()

	if len(tokens) != 3 { // include, string, EOF
		t.Fatalf("got %d tokens, want 3", len(tokens))
	}
	if tokens[0].Type != TokenIdent || tokens[0].Value != "include" {
		t.Errorf("token[0] = %v, want Ident(include)", tokens[0])
	}
	if tokens[1].Type != TokenString || tokens[1].Value != "us(basic)" {
		t.Errorf("token[1] = %v, want String(us(basic))", tokens[1])
	}
}

func TestLexerMinMax(t *testing.T) {
	input := `minimum = 8;
maximum = 255;`
	lexer := NewLexer([]byte(input))
	tokens := lexer.Tokenize()

	expected := []struct {
		typ   TokenType
		value string
	}{
		{TokenIdent, "minimum"},
		{TokenEquals, "="},
		{TokenNumber, "8"},
		{TokenSemicolon, ";"},
		{TokenIdent, "maximum"},
		{TokenEquals, "="},
		{TokenNumber, "255"},
		{TokenSemicolon, ";"},
		{TokenEOF, ""},
	}

	if len(tokens) != len(expected) {
		t.Fatalf("got %d tokens, want %d", len(tokens), len(expected))
	}

	for i, exp := range expected {
		if tokens[i].Type != exp.typ || tokens[i].Value != exp.value {
			t.Errorf("token[%d] = %v, want {%v, %q}", i, tokens[i], exp.typ, exp.value)
		}
	}
}

func TestLexerAlias(t *testing.T) {
	input := `alias <ALGR> = <RALT>;`
	lexer := NewLexer([]byte(input))
	tokens := lexer.Tokenize()

	expected := []TokenType{
		TokenIdent,     // alias
		TokenKeycode,   // ALGR
		TokenEquals,    // =
		TokenKeycode,   // RALT
		TokenSemicolon, // ;
		TokenEOF,
	}

	if len(tokens) != len(expected) {
		t.Fatalf("got %d tokens, want %d", len(tokens), len(expected))
	}

	for i, exp := range expected {
		if tokens[i].Type != exp {
			t.Errorf("token[%d].Type = %v, want %v", i, tokens[i].Type, exp)
		}
	}
}

func TestLexerIndicator(t *testing.T) {
	input := `indicator 1 = "Caps Lock";`
	lexer := NewLexer([]byte(input))
	tokens := lexer.Tokenize()

	expected := []struct {
		typ   TokenType
		value string
	}{
		{TokenIdent, "indicator"},
		{TokenNumber, "1"},
		{TokenEquals, "="},
		{TokenString, "Caps Lock"},
		{TokenSemicolon, ";"},
		{TokenEOF, ""},
	}

	if len(tokens) != len(expected) {
		t.Fatalf("got %d tokens, want %d", len(tokens), len(expected))
	}

	for i, exp := range expected {
		if tokens[i].Type != exp.typ || tokens[i].Value != exp.value {
			t.Errorf("token[%d] = %v, want {%v, %q}", i, tokens[i], exp.typ, exp.value)
		}
	}
}

func TestLexerSymbolsGroup(t *testing.T) {
	input := `symbols[Group1] = [ q, Q ]`
	lexer := NewLexer([]byte(input))
	tokens := lexer.Tokenize()

	expected := []TokenType{
		TokenIdent,    // symbols
		TokenLBracket, // [
		TokenIdent,    // Group1
		TokenRBracket, // ]
		TokenEquals,   // =
		TokenLBracket, // [
		TokenIdent,    // q
		TokenComma,    // ,
		TokenIdent,    // Q
		TokenRBracket, // ]
		TokenEOF,
	}

	if len(tokens) != len(expected) {
		t.Fatalf("got %d tokens, want %d", len(tokens), len(expected))
	}

	for i, exp := range expected {
		if tokens[i].Type != exp {
			t.Errorf("token[%d].Type = %v, want %v", i, tokens[i].Type, exp)
		}
	}
}

func TestLexerVirtualModifiers(t *testing.T) {
	input := `virtual_modifiers NumLock, Alt, LevelThree;`
	lexer := NewLexer([]byte(input))
	tokens := lexer.Tokenize()

	expected := []struct {
		typ   TokenType
		value string
	}{
		{TokenIdent, "virtual_modifiers"},
		{TokenIdent, "NumLock"},
		{TokenComma, ","},
		{TokenIdent, "Alt"},
		{TokenComma, ","},
		{TokenIdent, "LevelThree"},
		{TokenSemicolon, ";"},
		{TokenEOF, ""},
	}

	if len(tokens) != len(expected) {
		t.Fatalf("got %d tokens, want %d", len(tokens), len(expected))
	}

	for i, exp := range expected {
		if tokens[i].Type != exp.typ || tokens[i].Value != exp.value {
			t.Errorf("token[%d] = %v, want {%v, %q}", i, tokens[i], exp.typ, exp.value)
		}
	}
}

func TestLexerCommentEdgeCases(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []TokenType
	}{
		{
			"line comment at EOF",
			"foo // comment",
			[]TokenType{TokenIdent, TokenEOF},
		},
		{
			"block comment at EOF",
			"foo /* comment",
			[]TokenType{TokenIdent, TokenEOF},
		},
		{
			"slash not followed by comment",
			"foo / bar",
			[]TokenType{TokenIdent, TokenError},
		},
		{
			"multiple comments",
			"a // line\nb /* block */ c",
			[]TokenType{TokenIdent, TokenIdent, TokenIdent, TokenEOF},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lexer := NewLexer([]byte(tt.input))
			tokens := lexer.Tokenize()
			if len(tokens) != len(tt.want) {
				t.Fatalf("got %d tokens, want %d: %v", len(tokens), len(tt.want), tokens)
			}
			for i, exp := range tt.want {
				if tokens[i].Type != exp {
					t.Errorf("token[%d].Type = %v, want %v", i, tokens[i].Type, exp)
				}
			}
		})
	}
}

func TestLexerEOFInString(t *testing.T) {
	input := `"unterminated`
	lexer := NewLexer([]byte(input))
	tok := lexer.NextToken()
	if tok.Type != TokenError {
		t.Errorf("type = %v, want TokenError", tok.Type)
	}
}

func TestLexerOctalEscapePartial(t *testing.T) {
	// Octal escape with fewer than 3 digits followed by non-octal
	input := `"\17x"`
	lexer := NewLexer([]byte(input))
	tok := lexer.NextToken()
	if tok.Type != TokenString {
		t.Errorf("type = %v, want TokenString", tok.Type)
	}
	// \17 = 15 (octal), followed by 'x'
	expected := string(rune(017)) + "x"
	if tok.Value != expected {
		t.Errorf("value = %q, want %q", tok.Value, expected)
	}
}

func TestLexerAcceptNotInSet(t *testing.T) {
	lexer := NewLexer([]byte("ab"))
	// Try to accept a character not in the set
	if lexer.accept("xyz") {
		t.Error("accept() should return false for non-matching character")
	}
	// Position should not have advanced
	r := lexer.next()
	if r != 'a' {
		t.Errorf("next() = %q, want 'a'", r)
	}
}

func TestLexerBackupZeroWidth(t *testing.T) {
	lexer := NewLexer([]byte(""))
	// Read EOF (width = 0)
	r := lexer.next()
	if r != -1 {
		t.Errorf("next() = %q, want EOF", r)
	}
	// Backup with zero width should be no-op
	lexer.backup()
	// Should still be at EOF
	r = lexer.next()
	if r != -1 {
		t.Errorf("after backup: next() = %q, want EOF", r)
	}
}

func TestLexerEdgeCases(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"empty input", ""},
		{"only whitespace", "   \t\n\r   "},
		{"only comments", "// comment\n/* block */"},
		{"deeply nested braces", "{{{{{{}}}}}}"},
		{"many semicolons", ";;;;;;;"},
		{"mixed operators", "=+[]{}();,"},
		{"long identifier", strings.Repeat("a", 1000)},
		{"long string", `"` + strings.Repeat("x", 1000) + `"`},
		{"string with all escapes", `"\n\t\r\\\"\000\xff"`},
		{"invalid escape", `"\z"`},
		{"unterminated string", `"unterminated`},
		{"unterminated block comment", "/* never closed"},
		{"keycode at end", "<ABC"},
		{"keycode variations", "<A> <AB> <ABC> <ABCD> <AB01>"},
		{"numbers", "0 1 255 0x0 0xff 0xFF 0777 00"},
		{"hex upper lower", "0xABCDEF 0xabcdef"},
		{"large hex number", "0xFFFFFFFF"},
		{"negative looking", "-1"},
		{"special chars in string", `"\x00\x01\x1f"`},
		{"unicode in input", "// 日本語コメント"},
		{"consecutive strings", `"a""b""c"`},
		{"newline in weird places", "key\n<\nAD01\n>\n{\n}\n;"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lexer := NewLexer([]byte(tt.input))
			// Should not panic, collect all tokens
			var tokens []Token
			for {
				tok := lexer.NextToken()
				tokens = append(tokens, tok)
				if tok.Type == TokenEOF || tok.Type == TokenError {
					break
				}
				if len(tokens) > 10000 {
					t.Fatal("Too many tokens, possible infinite loop")
				}
			}
		})
	}
}
