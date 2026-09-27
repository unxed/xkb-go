package xkb

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// composeParser parses Compose files.
type composeParser struct {
	locale       string
	includePaths []string
	included     map[string]bool // tracks already included files to prevent cycles
	sequences    []composeSequence
	errors       []error
}

// composeSequence represents a single compose sequence.
type composeSequence struct {
	keysyms []Keysym
	result  composeResult
}

// newComposeParser creates a new compose file parser.
func newComposeParser(locale string, includePaths []string) *composeParser {
	return &composeParser{
		locale:       locale,
		includePaths: includePaths,
		included:     make(map[string]bool),
	}
}

// parseFile parses a compose file and its includes.
func (p *composeParser) parseFile(path string) error {
	// Resolve to absolute path
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("failed to resolve path %s: %w", path, err)
	}

	// Check for include cycle
	if p.included[absPath] {
		return nil // Already included
	}
	p.included[absPath] = true

	data, err := os.ReadFile(absPath)
	if err != nil {
		return fmt.Errorf("failed to read compose file %s: %w", path, err)
	}

	return p.parseData(data, absPath)
}

// parseData parses compose data.
func (p *composeParser) parseData(data []byte, sourcePath string) error {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Handle include directive
		if strings.HasPrefix(line, "include ") {
			includePath := p.parseIncludePath(line[8:])
			if includePath == "" {
				p.errors = append(p.errors, fmt.Errorf("%s:%d: invalid include directive", sourcePath, lineNum))
				continue
			}

			// Resolve include path
			resolvedPath := p.resolveIncludePath(includePath, sourcePath)
			if resolvedPath == "" {
				p.errors = append(p.errors, fmt.Errorf("%s:%d: include file not found: %s", sourcePath, lineNum, includePath))
				continue
			}

			if err := p.parseFile(resolvedPath); err != nil {
				p.errors = append(p.errors, fmt.Errorf("%s:%d: include error: %w", sourcePath, lineNum, err))
			}
			continue
		}

		// Parse compose sequence
		seq, err := p.parseLine(line)
		if err != nil {
			p.errors = append(p.errors, fmt.Errorf("%s:%d: %w", sourcePath, lineNum, err))
			continue
		}
		if seq != nil {
			p.sequences = append(p.sequences, *seq)
		}
	}

	return scanner.Err()
}

// parseIncludePath extracts the path from an include directive.
// Handles both "path" and %L substitution.
func (p *composeParser) parseIncludePath(s string) string {
	s = strings.TrimSpace(s)
	if len(s) < 2 {
		return ""
	}

	// Handle quoted path
	if s[0] == '"' {
		end := strings.Index(s[1:], "\"")
		if end < 0 {
			return ""
		}
		path := s[1 : end+1]
		// Handle %L substitution (locale)
		path = strings.ReplaceAll(path, "%L", p.locale)
		return path
	}

	return ""
}

// resolveIncludePath resolves an include path.
func (p *composeParser) resolveIncludePath(includePath, sourcePath string) string {
	// If absolute, use as-is
	if filepath.IsAbs(includePath) {
		if _, err := os.Stat(includePath); err == nil {
			return includePath
		}
		return ""
	}

	// Try relative to source file
	dir := filepath.Dir(sourcePath)
	candidate := filepath.Join(dir, includePath)
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}

	// Try include paths
	for _, base := range p.includePaths {
		candidate := filepath.Join(base, includePath)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}

	return ""
}

// parseLine parses a single compose sequence line.
// Format: <keysym1> <keysym2> ... : "result" keysym # comment
func (p *composeParser) parseLine(line string) (*composeSequence, error) {
	// Remove comment
	if idx := strings.Index(line, "#"); idx >= 0 {
		line = line[:idx]
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return nil, nil
	}

	// Split on colon
	parts := strings.SplitN(line, ":", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("missing ':' separator")
	}

	// Parse keysyms
	keysyms, err := p.parseKeysymSequence(strings.TrimSpace(parts[0]))
	if err != nil {
		return nil, err
	}
	if len(keysyms) == 0 {
		return nil, fmt.Errorf("empty keysym sequence")
	}

	// Parse result
	result, err := p.parseResult(strings.TrimSpace(parts[1]))
	if err != nil {
		return nil, err
	}

	return &composeSequence{
		keysyms: keysyms,
		result:  *result,
	}, nil
}

// parseKeysymSequence parses a sequence of keysyms like "<dead_acute> <a>".
func (p *composeParser) parseKeysymSequence(s string) ([]Keysym, error) {
	var keysyms []Keysym

	for len(s) > 0 {
		s = strings.TrimSpace(s)
		if s == "" {
			break
		}

		if s[0] != '<' {
			return nil, fmt.Errorf("expected '<' in keysym sequence")
		}

		end := strings.Index(s, ">")
		if end < 0 {
			return nil, fmt.Errorf("unterminated keysym")
		}

		name := s[1:end]
		ks := p.lookupKeysym(name)
		if ks == KeyNoSymbol {
			return nil, fmt.Errorf("unknown keysym: %s", name)
		}
		keysyms = append(keysyms, ks)

		s = s[end+1:]
	}

	return keysyms, nil
}

// parseResult parses the result part of a compose line.
// Format: "string" keysym (string and/or keysym may be present)
func (p *composeParser) parseResult(s string) (*composeResult, error) {
	result := &composeResult{}

	s = strings.TrimSpace(s)

	// Parse quoted string
	if len(s) > 0 && s[0] == '"' {
		str, rest, err := p.parseQuotedString(s)
		if err != nil {
			return nil, err
		}
		result.utf8 = str
		s = strings.TrimSpace(rest)
	}

	// Parse keysym name (if present)
	if len(s) > 0 && s[0] != '#' {
		// Find end of keysym name
		end := strings.IndexAny(s, " \t#")
		if end < 0 {
			end = len(s)
		}
		name := s[:end]
		if name != "" {
			ks := p.lookupKeysym(name)
			if ks != KeyNoSymbol {
				result.keysym = ks
			}
		}
	}

	// If no keysym but we have a string, derive keysym from string
	if result.keysym == KeyNoSymbol && result.utf8 != "" {
		runes := []rune(result.utf8)
		if len(runes) == 1 {
			result.keysym = UTF32ToKeysym(runes[0])
		}
	}

	// If no string but we have a keysym, derive string from keysym
	if result.utf8 == "" && result.keysym != KeyNoSymbol {
		result.utf8 = KeysymToUTF8(result.keysym)
	}

	return result, nil
}

// parseQuotedString parses a quoted string with escape sequences.
func (p *composeParser) parseQuotedString(s string) (string, string, error) {
	if len(s) == 0 || s[0] != '"' {
		return "", s, fmt.Errorf("expected quoted string")
	}

	var result strings.Builder
	i := 1
	for i < len(s) {
		if s[i] == '"' {
			return result.String(), s[i+1:], nil
		}
		if s[i] == '\\' && i+1 < len(s) {
			i++
			switch s[i] {
			case 'n':
				result.WriteByte('\n')
			case 't':
				result.WriteByte('\t')
			case 'r':
				result.WriteByte('\r')
			case '\\':
				result.WriteByte('\\')
			case '"':
				result.WriteByte('"')
			case '0', '1', '2', '3', '4', '5', '6', '7':
				// Octal escape
				val := 0
				for j := 0; j < 3 && i < len(s) && s[i] >= '0' && s[i] <= '7'; j++ {
					val = val*8 + int(s[i]-'0')
					i++
				}
				result.WriteByte(byte(val))
				continue
			case 'x':
				// Hex escape
				i++
				if i+2 <= len(s) {
					if val, err := strconv.ParseUint(s[i:i+2], 16, 8); err == nil {
						result.WriteByte(byte(val))
						i += 2
						continue
					}
				}
				result.WriteByte('x')
				continue
			default:
				result.WriteByte(s[i])
			}
			i++
		} else {
			result.WriteByte(s[i])
			i++
		}
	}

	return "", "", fmt.Errorf("unterminated string")
}

// lookupKeysym looks up a keysym by name.
// Handles standard names, UXXXX notation, and single characters.
func (p *composeParser) lookupKeysym(name string) Keysym {
	// Try direct lookup
	if ks := KeysymFromName(name, KeysymNameNoFlags); ks != KeyNoSymbol {
		return ks
	}

	// Try UXXXX notation (Unicode code point)
	if len(name) >= 2 && name[0] == 'U' {
		if val, err := strconv.ParseUint(name[1:], 16, 32); err == nil {
			// Return Unicode keysym format
			if val >= 0x100 && val <= 0x10ffff {
				return Keysym(val + 0x01000000)
			}
			if val <= 0xff {
				return Keysym(val)
			}
		}
	}

	// Try 0xXXXX notation (direct keysym value)
	if strings.HasPrefix(name, "0x") || strings.HasPrefix(name, "0X") {
		if val, err := strconv.ParseUint(name[2:], 16, 32); err == nil {
			return Keysym(val)
		}
	}

	// For compose files, keysyms might use different names
	// Let's add some common aliases
	aliases := map[string]Keysym{
		// Dead keys with underscore variants
		"dead_acute":            KeyDeadAcute,
		"dead_grave":            KeyDeadGrave,
		"dead_circumflex":       KeyDeadCircumflex,
		"dead_tilde":            KeyDeadTilde,
		"dead_macron":           KeyDeadMacron,
		"dead_breve":            KeyDeadBreve,
		"dead_abovedot":         KeyDeadAbovedot,
		"dead_diaeresis":        KeyDeadDiaeresis,
		"dead_abovering":        KeyDeadAbovering,
		"dead_doubleacute":      KeyDeadDoubleacute,
		"dead_caron":            KeyDeadCaron,
		"dead_cedilla":          KeyDeadCedilla,
		"dead_ogonek":           KeyDeadOgonek,
		"dead_iota":             KeyDeadIota,
		"dead_voiced_sound":     KeyDeadVoicedSound,
		"dead_semivoiced_sound": KeyDeadSemivoicedSound,
		"dead_belowdot":         KeyDeadBelowdot,
		"Multi_key":             KeyMultiKey,

		// Common names
		"NoSymbol":       KeyNoSymbol,
		"VoidSymbol":     KeyNoSymbol,
		"space":          0x0020,
		"exclam":         '!',
		"quotedbl":       '"',
		"numbersign":     '#',
		"dollar":         '$',
		"percent":        '%',
		"ampersand":      '&',
		"apostrophe":     '\'',
		"quoteright":     '\'',
		"parenleft":      '(',
		"parenright":     ')',
		"asterisk":       '*',
		"plus":           '+',
		"comma":          ',',
		"minus":          '-',
		"period":         '.',
		"slash":          '/',
		"colon":          ':',
		"semicolon":      ';',
		"less":           '<',
		"equal":          '=',
		"greater":        '>',
		"question":       '?',
		"at":             '@',
		"bracketleft":    '[',
		"backslash":      '\\',
		"bracketright":   ']',
		"asciicircum":    '^',
		"underscore":     '_',
		"grave":          '`',
		"quoteleft":      '`',
		"braceleft":      '{',
		"bar":            '|',
		"braceright":     '}',
		"asciitilde":     '~',
		"nobreakspace":   0x00a0,
		"exclamdown":     0x00a1,
		"cent":           0x00a2,
		"sterling":       0x00a3,
		"currency":       0x00a4,
		"yen":            0x00a5,
		"brokenbar":      0x00a6,
		"section":        0x00a7,
		"diaeresis":      0x00a8,
		"copyright":      0x00a9,
		"ordfeminine":    0x00aa,
		"guillemotleft":  0x00ab,
		"notsign":        0x00ac,
		"hyphen":         0x00ad,
		"registered":     0x00ae,
		"macron":         0x00af,
		"degree":         0x00b0,
		"plusminus":      0x00b1,
		"twosuperior":    0x00b2,
		"threesuperior":  0x00b3,
		"acute":          0x00b4,
		"mu":             0x00b5,
		"paragraph":      0x00b6,
		"periodcentered": 0x00b7,
		"cedilla":        0x00b8,
		"onesuperior":    0x00b9,
		"masculine":      0x00ba,
		"guillemotright": 0x00bb,
		"onequarter":     0x00bc,
		"onehalf":        0x00bd,
		"threequarters":  0x00be,
		"questiondown":   0x00bf,
	}

	if ks, ok := aliases[name]; ok {
		return ks
	}

	// Single character
	if len(name) == 1 {
		return Keysym(name[0])
	}

	return KeyNoSymbol
}

// buildComposeTable builds a ComposeTable from parsed sequences.
func (p *composeParser) buildComposeTable() *ComposeTable {
	root := &composeNode{
		children: make(map[Keysym]*composeNode),
	}

	for _, seq := range p.sequences {
		node := root
		for _, ks := range seq.keysyms {
			if node.children == nil {
				node.children = make(map[Keysym]*composeNode)
			}
			child, ok := node.children[ks]
			if !ok {
				child = &composeNode{
					children: make(map[Keysym]*composeNode),
				}
				node.children[ks] = child
			}
			node = child
		}
		// Set result (later definitions override earlier ones)
		node.result = &composeResult{
			keysym: seq.result.keysym,
			utf8:   seq.result.utf8,
		}
	}

	return &ComposeTable{
		locale: p.locale,
		root:   root,
	}
}
