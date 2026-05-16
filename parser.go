package xkb

import (
	"fmt"
	"strconv"
	"strings"
)

// Parser parses XKB text format into a [Keymap].
//
// Create with [NewParser], then call [Parser.Parse] to parse the input.
// The Parser is used internally by [Context.NewKeymapFromString].
type Parser struct {
	lexer   *Lexer
	current Token
	prev    Token
	errors  []error

	// Track which sections have been parsed (for duplicate detection)
	sawKeycodes bool
	sawTypes    bool
	sawCompat   bool
	sawSymbols  bool

	// Compat section defaults
	interpretRepeatDefault    bool // Default repeat value for interpret statements
	interpretRepeatDefaultSet bool // Was interpret.repeat explicitly set in compat?

	groupOffset int // Current group offset for symbols
}

// NewParser creates a new [Parser] for the given XKB source input.
func NewParser(input []byte) *Parser {
	p := &Parser{
		lexer:                  NewLexer(input),
		interpretRepeatDefault: true, // Keys repeat by default
	}
	p.advance() // Prime the parser with the first token
	return p
}

// advance moves to the next token.
func (p *Parser) advance() Token {
	p.prev = p.current
	p.current = p.lexer.NextToken()
	return p.current
}

// check returns true if the current token is of the given type.
func (p *Parser) check(t TokenType) bool {
	return p.current.Type == t
}

// match consumes the current token if it matches any of the given types.
func (p *Parser) match(types ...TokenType) bool {
	for _, t := range types {
		if p.check(t) {
			p.advance()
			return true
		}
	}
	return false
}

// expect consumes the current token if it matches, otherwise returns an error.
func (p *Parser) expect(t TokenType) error {
	if p.check(t) {
		p.advance()
		return nil
	}
	return p.errorf("expected %s, got %s", t, p.current.Type)
}

// expectIdent consumes an identifier and returns its value.
func (p *Parser) expectIdent() (string, error) {
	if !p.check(TokenIdent) {
		return "", p.errorf("expected identifier, got %s", p.current.Type)
	}
	val := p.current.Value
	p.advance()
	return val, nil
}

// expectString consumes a string and returns its value.
func (p *Parser) expectString() (string, error) {
	if !p.check(TokenString) {
		return "", p.errorf("expected string, got %s", p.current.Type)
	}
	val := p.current.Value
	p.advance()
	return val, nil
}

// expectNumber consumes a number and returns its value.
func (p *Parser) expectNumber() (int, error) {
	if !p.check(TokenNumber) {
		return 0, p.errorf("expected number, got %s", p.current.Type)
	}
	val, err := p.parseNumber(p.current.Value)
	if err != nil {
		return 0, p.errorf("invalid number: %s", p.current.Value)
	}
	p.advance()
	return val, nil
}

// expectKeycode consumes a keycode and returns its name.
func (p *Parser) expectKeycode() (string, error) {
	if !p.check(TokenKeycode) {
		return "", p.errorf("expected keycode, got %s", p.current.Type)
	}
	val := p.current.Value
	p.advance()
	return val, nil
}

// parseNumber parses a number string (decimal, hex, or octal).
func (p *Parser) parseNumber(s string) (int, error) {
	if strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0X") {
		val, err := strconv.ParseInt(s[2:], 16, 64)
		return int(val), err
	}
	if len(s) > 1 && s[0] == '0' {
		val, err := strconv.ParseInt(s, 8, 64)
		return int(val), err
	}
	val, err := strconv.ParseInt(s, 10, 64)
	return int(val), err
}

// errorf creates a parse error with location information.
func (p *Parser) errorf(format string, args ...any) error {
	return &Error{
		Op:   "parse",
		Line: p.current.Line,
		Col:  p.current.Col,
		Err:  fmt.Errorf(format, args...),
	}
}

// addError adds an error to the list.
func (p *Parser) addError(err error) {
	p.errors = append(p.errors, err)
}

// skipToSemicolonOrBrace skips tokens until a semicolon or closing brace is found.
// Used for error recovery.
func (p *Parser) skipToSemicolonOrBrace() {
	for {
		switch p.current.Type {
		case TokenEOF, TokenSemicolon, TokenRBrace:
			return
		}
		p.advance()
	}
}

// skipStatementWithBraces skips a statement, properly handling nested braces.
// It continues until it finds a semicolon at depth 0.
func (p *Parser) skipStatementWithBraces() {
	depth := 0
	for {
		switch p.current.Type {
		case TokenEOF:
			return
		case TokenLBrace:
			depth++
		case TokenRBrace:
			depth--
		case TokenSemicolon:
			if depth <= 0 {
				return
			}
		}
		p.advance()
	}
}

// Parse parses a complete XKB keymap.
func (p *Parser) Parse() (*Keymap, error) {
	keymap := &Keymap{
		keycodeNames:   make(map[Keycode]string),
		keycodesByName: make(map[string]Keycode),
		types:          make(map[string]*KeyType),
		keys:           make(map[Keycode]*Key),
		virtualMods:    make(map[string]ModMask),
		leds:           make(map[string]*LED),
		groupNames:     make([]string, 4),
		numGroups:      1,
		minKeycode:     8,
		maxKeycode:     255,
	}

	// Expect: xkb_keymap {
	if ident, err := p.expectIdent(); err != nil {
		return nil, err
	} else if ident != "xkb_keymap" {
		return nil, p.errorf("expected xkb_keymap, got %s", ident)
	}

	if err := p.expect(TokenLBrace); err != nil {
		return nil, err
	}

	// Parse sections until }
	for !p.check(TokenRBrace) && !p.check(TokenEOF) {
		if err := p.parseSection(keymap); err != nil {
			p.addError(err)
			p.skipToSemicolonOrBrace()
			if p.check(TokenSemicolon) {
				p.advance()
			}
		}
	}

	if err := p.expect(TokenRBrace); err != nil {
		return nil, err
	}

	// Optional trailing semicolon
	p.match(TokenSemicolon)

	// Apply interpret statements to keys (for repeat settings, etc.)
	p.applyInterprets(keymap)

	// Validate the keymap
	p.validateKeymap(keymap)

	if len(p.errors) > 0 {
		return keymap, p.errors[0] // Return first error
	}

	return keymap, nil
}

// validateKeymap performs semantic validation on the parsed keymap.
// This matches libxkbcommon's validation behavior.
func (p *Parser) validateKeymap(keymap *Keymap) {
	// Check that all required sections were present
	// Note: libxkbcommon now treats sections as optional, but we follow
	// the stricter approach of requiring all 4 main sections.
	if !p.sawKeycodes {
		p.addError(fmt.Errorf("keymap is missing xkb_keycodes section"))
	}
	if !p.sawTypes {
		p.addError(fmt.Errorf("keymap is missing xkb_types section"))
	}
	if !p.sawCompat {
		p.addError(fmt.Errorf("keymap is missing xkb_compat section"))
	}
	if !p.sawSymbols {
		p.addError(fmt.Errorf("keymap is missing xkb_symbols section"))
	}

	// Validate that keys reference valid types and assign defaults
	for _, key := range keymap.keys {
		// Check each group's type reference
		for groupIdx, group := range key.groups {
			if group.keyType == nil {
				// Assign default ONE_LEVEL type if missing
				if defaultType, ok := keymap.types["ONE_LEVEL"]; ok {
					key.groups[groupIdx].keyType = defaultType
				}
			} else {
				// Verify the type exists in the keymap, resolve to default if not
				if _, ok := keymap.types[group.keyType.name]; !ok {
					// Try to resolve "default" or unknown types to a sensible default
					numLevels := len(group.levels)
					var defaultTypeName string
					switch numLevels {
					case 1:
						defaultTypeName = "ONE_LEVEL"
					case 2:
						defaultTypeName = "TWO_LEVEL"
					case 4:
						defaultTypeName = "FOUR_LEVEL"
					default:
						defaultTypeName = "ONE_LEVEL"
					}
					if resolvedType, ok := keymap.types[defaultTypeName]; ok {
						key.groups[groupIdx].keyType = resolvedType
					}
					// Don't error - just use the best available default
				}
			}
		}
	}
}

// parseSection parses a single xkb section (keycodes, types, compat, symbols, geometry).
func (p *Parser) parseSection(keymap *Keymap) error {
	sectionName, err := p.expectIdent()
	if err != nil {
		return err
	}

	switch sectionName {
	case "xkb_keycodes":
		if p.sawKeycodes {
			return p.errorf("more than one xkb_keycodes section in keymap")
		}
		p.sawKeycodes = true
		return p.parseKeycodes(keymap)
	case "xkb_types":
		if p.sawTypes {
			return p.errorf("more than one xkb_types section in keymap")
		}
		p.sawTypes = true
		return p.parseTypes(keymap)
	case "xkb_compat", "xkb_compatibility":
		if p.sawCompat {
			return p.errorf("more than one xkb_compat section in keymap")
		}
		p.sawCompat = true
		return p.parseCompat(keymap)
	case "xkb_symbols":
		if p.sawSymbols {
			return p.errorf("more than one xkb_symbols section in keymap")
		}
		p.sawSymbols = true
		return p.parseSymbols(keymap)
	case "xkb_geometry":
		return p.skipSection() // Ignored
	default:
		return p.errorf("unknown section: %s", sectionName)
	}
}

// skipSection skips an entire section (used for xkb_geometry).
func (p *Parser) skipSection() error {
	// Skip optional name string
	p.match(TokenString)

	// Expect {
	if err := p.expect(TokenLBrace); err != nil {
		return err
	}

	// Skip until matching }
	depth := 1
	for depth > 0 && !p.check(TokenEOF) {
		if p.check(TokenLBrace) {
			depth++
		} else if p.check(TokenRBrace) {
			depth--
		}
		p.advance()
	}

	// Expect ;
	return p.expect(TokenSemicolon)
}

// parseKeycodes parses the xkb_keycodes section.
func (p *Parser) parseKeycodes(keymap *Keymap) error {
	// Optional section name
	p.match(TokenString)

	// Expect {
	if err := p.expect(TokenLBrace); err != nil {
		return err
	}

	// Parse statements until }
	for !p.check(TokenRBrace) && !p.check(TokenEOF) {
		if err := p.parseKeycodesStatement(keymap); err != nil {
			p.addError(err)
			p.skipToSemicolonOrBrace()
		}
		if p.check(TokenSemicolon) {
			p.advance()
		}
	}

	if err := p.expect(TokenRBrace); err != nil {
		return err
	}

	return p.expect(TokenSemicolon)
}

// parseKeycodesStatement parses a single statement in xkb_keycodes.
func (p *Parser) parseKeycodesStatement(keymap *Keymap) error {
	// Check for keycode definition: <NAME> = number
	if p.check(TokenKeycode) {
		return p.parseKeycodeDefinition(keymap)
	}

	// Must be an identifier (minimum, maximum, alias, indicator)
	ident, err := p.expectIdent()
	if err != nil {
		return err
	}

	switch ident {
	case "minimum":
		if err := p.expect(TokenEquals); err != nil {
			return err
		}
		min, err := p.expectNumber()
		if err != nil {
			return err
		}
		keymap.minKeycode = Keycode(min)
		return nil

	case "maximum":
		if err := p.expect(TokenEquals); err != nil {
			return err
		}
		max, err := p.expectNumber()
		if err != nil {
			return err
		}
		keymap.maxKeycode = Keycode(max)
		return nil

	case "alias":
		return p.parseKeycodeAlias(keymap)

	case "indicator":
		return p.parseIndicatorName(keymap)

	case "virtual":
		// virtual indicators - skip for now
		p.skipToSemicolonOrBrace()
		return nil

	default:
		return p.errorf("unexpected identifier in xkb_keycodes: %s", ident)
	}
}

// parseKeycodeDefinition parses: <NAME> = number
func (p *Parser) parseKeycodeDefinition(keymap *Keymap) error {
	name, err := p.expectKeycode()
	if err != nil {
		return err
	}

	if err := p.expect(TokenEquals); err != nil {
		return err
	}

	code, err := p.expectNumber()
	if err != nil {
		return err
	}

	keycode := Keycode(code)
	keymap.keycodeNames[keycode] = name
	keymap.keycodesByName[name] = keycode

	// Auto-expand the range if this keycode is outside declared min/max
	if keycode < keymap.minKeycode {
		keymap.minKeycode = keycode
	}
	if keycode > keymap.maxKeycode {
		keymap.maxKeycode = keycode
	}

	return nil
}

// parseKeycodeAlias parses: alias <NAME> = <TARGET>
func (p *Parser) parseKeycodeAlias(keymap *Keymap) error {
	aliasName, err := p.expectKeycode()
	if err != nil {
		return err
	}

	if err := p.expect(TokenEquals); err != nil {
		return err
	}

	targetName, err := p.expectKeycode()
	if err != nil {
		return err
	}

	// Look up the target keycode
	if targetCode, ok := keymap.keycodesByName[targetName]; ok {
		keymap.keycodesByName[aliasName] = targetCode
	}
	// If target not found, the alias still gets recorded (may be resolved later)

	return nil
}

// parseIndicatorName parses: indicator number = "Name"
func (p *Parser) parseIndicatorName(keymap *Keymap) error {
	index, err := p.expectNumber()
	if err != nil {
		return err
	}

	if err := p.expect(TokenEquals); err != nil {
		return err
	}

	name, err := p.expectString()
	if err != nil {
		return err
	}

	keymap.leds[name] = &LED{
		name:  name,
		index: index,
	}

	return nil
}

// parseTypes parses the xkb_types section.
func (p *Parser) parseTypes(keymap *Keymap) error {
	// Optional section name
	p.match(TokenString)

	// Expect {
	if err := p.expect(TokenLBrace); err != nil {
		return err
	}

	// Parse statements until }
	for !p.check(TokenRBrace) && !p.check(TokenEOF) {
		if err := p.parseTypesStatement(keymap); err != nil {
			p.addError(err)
			p.skipToSemicolonOrBrace()
		}
		if p.check(TokenSemicolon) {
			p.advance()
		}
	}

	if err := p.expect(TokenRBrace); err != nil {
		return err
	}

	return p.expect(TokenSemicolon)
}

// parseTypesStatement parses a single statement in xkb_types.
func (p *Parser) parseTypesStatement(keymap *Keymap) error {
	ident, err := p.expectIdent()
	if err != nil {
		return err
	}

	switch ident {
	case "virtual_modifiers":
		return p.parseVirtualModifiers(keymap)
	case "type":
		return p.parseTypeDefinition(keymap)
	default:
		return p.errorf("unexpected identifier in xkb_types: %s", ident)
	}
}

// parseVirtualModifiers parses: virtual_modifiers Name1, Name2, Name3=0x4000, ...;
func (p *Parser) parseVirtualModifiers(keymap *Keymap) error {
	for {
		name, err := p.expectIdent()
		if err != nil {
			return err
		}

		// Check for optional =value assignment (e.g., Hyper=0x4000)
		var mask ModMask
		if p.match(TokenEquals) {
			num, err := p.expectNumber()
			if err != nil {
				return err
			}
			mask = ModMask(num)
		}

		// Assign a virtual modifier
		if _, exists := keymap.virtualMods[name]; !exists {
			keymap.virtualMods[name] = mask
		}

		if !p.match(TokenComma) {
			break
		}
	}
	return nil
}

// parseTypeDefinition parses: type "NAME" { ... }
func (p *Parser) parseTypeDefinition(keymap *Keymap) error {
	name, err := p.expectString()
	if err != nil {
		return err
	}

	if err := p.expect(TokenLBrace); err != nil {
		return err
	}

	keyType := &KeyType{
		name:      name,
		numLevels: 1,
	}

	// Parse type body
	for !p.check(TokenRBrace) && !p.check(TokenEOF) {
		if err := p.parseTypeStatement(keyType, keymap); err != nil {
			p.addError(err)
			p.skipToSemicolonOrBrace()
		}
		if p.check(TokenSemicolon) {
			p.advance()
		}
	}

	if err := p.expect(TokenRBrace); err != nil {
		return err
	}

	keymap.types[name] = keyType
	keymap.typesList = append(keymap.typesList, keyType)
	return nil
}

// parseTypeStatement parses a single statement in a type definition.
func (p *Parser) parseTypeStatement(keyType *KeyType, keymap *Keymap) error {
	ident, err := p.expectIdent()
	if err != nil {
		return err
	}

	switch ident {
	case "modifiers":
		if err := p.expect(TokenEquals); err != nil {
			return err
		}
		mods, err := p.parseModifierMask(keymap)
		if err != nil {
			return err
		}
		keyType.mods = mods
		return nil

	case "map":
		return p.parseTypeMapEntry(keyType, keymap)

	case "level_name":
		return p.parseTypeLevelName(keyType)

	case "preserve":
		// Skip preserve statements for now
		p.skipToSemicolonOrBrace()
		return nil

	default:
		return p.errorf("unexpected identifier in type: %s", ident)
	}
}

// parseModifierMask parses a modifier expression like: Shift + Lock or none
func (p *Parser) parseModifierMask(keymap *Keymap) (ModMask, error) {
	var mask ModMask

	for {
		if p.check(TokenIdent) {
			name := p.current.Value
			p.advance()

			if name == "none" || name == "None" {
				return 0, nil
			}

			modMask := p.modifierNameToMask(name, keymap)
			mask |= modMask
		} else {
			return 0, p.errorf("expected modifier name")
		}

		if !p.match(TokenPlus) {
			break
		}
	}

	return mask, nil
}

// modifierNameToMask converts a modifier name to its mask.
func (p *Parser) modifierNameToMask(name string, keymap *Keymap) ModMask {
	switch name {
	case "Shift":
		return ModShift
	case "Lock":
		return ModLock
	case "Control":
		return ModControl
	case "Mod1":
		return ModMod1
	case "Mod2":
		return ModMod2
	case "Mod3":
		return ModMod3
	case "Mod4":
		return ModMod4
	case "Mod5":
		return ModMod5
	// Common virtual modifier names and their standard real modifier mappings
	case "Alt", "Meta":
		return ModMod1
	case "NumLock":
		return ModMod2
	case "Super", "Hyper":
		return ModMod4
	case "LevelThree", "ISO_Level3_Shift", "AltGr":
		return ModMod5
	default:
		// Check virtual modifiers (for custom mappings)
		if mask, ok := keymap.virtualMods[name]; ok && mask != 0 {
			return mask
		}
		return 0
	}
}

// parseTypeMapEntry parses: map[MODS] = LevelN or map[MODS] = N
func (p *Parser) parseTypeMapEntry(keyType *KeyType, keymap *Keymap) error {
	if err := p.expect(TokenLBracket); err != nil {
		return err
	}

	mods, err := p.parseModifierMask(keymap)
	if err != nil {
		return err
	}

	if err := p.expect(TokenRBracket); err != nil {
		return err
	}

	if err := p.expect(TokenEquals); err != nil {
		return err
	}

	// Handle both "Level2" and numeric "2"
	var level Level
	if p.check(TokenNumber) {
		num, err := p.expectNumber()
		if err != nil {
			return err
		}
		level = Level(num - 1) // Convert 1-based to 0-based
	} else {
		levelName, err := p.expectIdent()
		if err != nil {
			return err
		}
		level, err = p.parseLevelName(levelName)
		if err != nil {
			return err
		}
	}

	keyType.entries = append(keyType.entries, KeyTypeEntry{
		mods:  mods,
		level: level,
	})

	if int(level)+1 > keyType.numLevels {
		keyType.numLevels = int(level) + 1
	}

	return nil
}

// parseLevelName parses a level name like "Level1" or "Level2".
func (p *Parser) parseLevelName(name string) (Level, error) {
	if !strings.HasPrefix(name, "Level") {
		return 0, fmt.Errorf("invalid level name: %s", name)
	}
	numStr := name[5:]
	num, err := strconv.Atoi(numStr)
	if err != nil {
		return 0, fmt.Errorf("invalid level number: %s", name)
	}
	return Level(num - 1), nil // Convert 1-based to 0-based
}

// parseTypeLevelName parses: level_name[LevelN] = "Name" or level_name[N] = "Name"
func (p *Parser) parseTypeLevelName(keyType *KeyType) error {
	if err := p.expect(TokenLBracket); err != nil {
		return err
	}

	// Handle both "Level1" and numeric "1" indices
	var level Level
	if p.check(TokenNumber) {
		num, err := p.expectNumber()
		if err != nil {
			return err
		}
		level = Level(num - 1) // Convert 1-based to 0-based
	} else {
		levelName, err := p.expectIdent()
		if err != nil {
			return err
		}
		var err2 error
		level, err2 = p.parseLevelName(levelName)
		if err2 != nil {
			return err2
		}
	}

	if err := p.expect(TokenRBracket); err != nil {
		return err
	}

	if err := p.expect(TokenEquals); err != nil {
		return err
	}

	_, err := p.expectString()
	if err != nil {
		return err
	}

	// Level names are informational, we don't store them
	// But we do update numLevels
	if int(level)+1 > keyType.numLevels {
		keyType.numLevels = int(level) + 1
	}

	return nil
}

// parseCompat parses the xkb_compat section.
func (p *Parser) parseCompat(keymap *Keymap) error {
	// Optional section name
	p.match(TokenString)

	// Expect {
	if err := p.expect(TokenLBrace); err != nil {
		return err
	}

	// Parse statements until }
	for !p.check(TokenRBrace) && !p.check(TokenEOF) {
		if err := p.parseCompatStatement(keymap); err != nil {
			p.addError(err)
			p.skipToSemicolonOrBrace()
		}
		if p.check(TokenSemicolon) {
			p.advance()
		}
	}

	if err := p.expect(TokenRBrace); err != nil {
		return err
	}

	return p.expect(TokenSemicolon)
}

// parseCompatStatement parses a single statement in xkb_compat.
func (p *Parser) parseCompatStatement(keymap *Keymap) error {
	ident, err := p.expectIdent()
	if err != nil {
		return err
	}

	switch ident {
	case "virtual_modifiers":
		return p.parseVirtualModifiers(keymap)
	case "interpret":
		// Check if this is "interpret.property = value" (default setting)
		// or "interpret Keysym { ... }" (interpret statement)
		if p.check(TokenDot) {
			return p.parseInterpretDefault()
		}
		return p.parseInterpret(keymap)
	case "indicator":
		return p.parseCompatIndicator(keymap)
	case "group":
		// Skip group statements
		p.skipToSemicolonOrBrace()
		return nil
	default:
		// Skip unknown statements (action defaults like setMods.clearLocks, etc.)
		p.skipToSemicolonOrBrace()
		return nil
	}
}

// parseInterpretDefault parses: interpret.property = value;
// For example: interpret.repeat = False;
func (p *Parser) parseInterpretDefault() error {
	p.advance() // consume .

	prop, err := p.expectIdent()
	if err != nil {
		return err
	}

	if err := p.expect(TokenEquals); err != nil {
		return err
	}

	switch prop {
	case "repeat":
		val, err := p.expectIdent()
		if err != nil {
			return err
		}
		switch strings.ToLower(val) {
		case "true", "yes":
			p.interpretRepeatDefault = true
			p.interpretRepeatDefaultSet = true
		case "false", "no":
			p.interpretRepeatDefault = false
			p.interpretRepeatDefaultSet = true
		}
	default:
		// Skip unknown properties (like useModMapMods, locking, etc.)
		p.skipToSemicolonOrBrace()
	}

	return nil
}

// parseInterpret parses: interpret KeysymName { ... } or interpret 0xNNNN+ModMatch(mods) { ... }
func (p *Parser) parseInterpret(keymap *Keymap) error {
	interp := &Interpret{}

	// Parse keysym - can be identifier (name) or number (hex value)
	if p.check(TokenNumber) {
		// Numeric keysym like 0xff7f
		numStr := p.current.Value
		p.advance()
		num, err := p.parseNumber(numStr)
		if err != nil {
			return err
		}
		interp.keysym = Keysym(num)
	} else {
		keysymName, err := p.expectIdent()
		if err != nil {
			return err
		}

		// Handle "Any" keysym
		if keysymName == "Any" {
			interp.keysym = KeyNoSymbol // KeyNoSymbol means "match any"
		} else {
			// Look up the keysym by name
			sym := KeysymFromName(keysymName, KeysymNameNoFlags)
			if sym == KeyNoSymbol {
				// Unknown keysym, skip this interpret
				p.skipStatementWithBraces()
				return nil
			}
			interp.keysym = sym
		}
	}

	// Check for modifier match: +AnyOf(...), +Exactly(...), etc.
	if p.check(TokenPlus) {
		p.advance() // consume +
		interp.modMatch, interp.mods = p.parseModMatch()
	}

	// Expect {
	if !p.check(TokenLBrace) {
		// No body, skip to semicolon
		p.skipToSemicolonOrBrace()
		return nil
	}
	p.advance() // consume {

	// Parse interpret body
	for !p.check(TokenRBrace) && !p.check(TokenEOF) {
		if p.check(TokenIdent) {
			ident := p.current.Value
			p.advance()

			if p.check(TokenEquals) {
				p.advance() // consume =

				switch ident {
				case "repeat":
					if p.check(TokenIdent) {
						val := p.current.Value
						p.advance()
						switch strings.ToLower(val) {
						case "true", "yes":
							repeatVal := true
							interp.repeat = &repeatVal
						case "false", "no":
							repeatVal := false
							interp.repeat = &repeatVal
						}
					}
				default:
					// Skip other properties (action, locking, etc.)
					p.skipToSemicolonOrBrace()
				}
			} else {
				// Not an assignment, skip
				p.skipToSemicolonOrBrace()
			}
		} else {
			p.skipToSemicolonOrBrace()
		}

		if p.check(TokenSemicolon) {
			p.advance()
		}
	}

	if err := p.expect(TokenRBrace); err != nil {
		return err
	}

	// Store the interpret
	keymap.interprets = append(keymap.interprets, interp)

	return nil
}

// parseModMatch parses modifier match expressions like AnyOf(Shift+Lock), Exactly(Control), etc.
func (p *Parser) parseModMatch() (ModMatch, ModMask) {
	if !p.check(TokenIdent) {
		return ModMatchNone, 0
	}

	matchType := p.current.Value
	p.advance()

	var modMatch ModMatch
	switch matchType {
	case "AnyOfOrNone":
		modMatch = ModMatchAnyOfOrNone
	case "AnyOf":
		modMatch = ModMatchAnyOf
	case "NoneOf":
		modMatch = ModMatchNoneOf
	case "AllOf":
		modMatch = ModMatchAllOf
	case "Exactly":
		modMatch = ModMatchExactly
	case "Any":
		// "Any" without parens means match any modifier state
		return ModMatchAnyOfOrNone, 0
	default:
		// Could be a modifier name directly (e.g., +Lock)
		return ModMatchExactly, p.modNameToMask(matchType)
	}

	// Parse (modifiers)
	if !p.check(TokenLParen) {
		return modMatch, 0
	}
	p.advance() // consume (

	var mods ModMask
	for !p.check(TokenRParen) && !p.check(TokenEOF) {
		if p.check(TokenIdent) {
			mods |= p.modNameToMask(p.current.Value)
			p.advance()
		}
		if p.check(TokenPlus) {
			p.advance()
		}
	}

	if p.check(TokenRParen) {
		p.advance()
	}

	return modMatch, mods
}

// modNameToMask converts a modifier name to its mask.
func (p *Parser) modNameToMask(name string) ModMask {
	switch name {
	case "Shift":
		return ModShift
	case "Lock", "Caps":
		return ModLock
	case "Control", "Ctrl":
		return ModControl
	case "Mod1", "Alt":
		return ModMod1
	case "Mod2", "NumLock":
		return ModMod2
	case "Mod3":
		return ModMod3
	case "Mod4", "Super":
		return ModMod4
	case "Mod5":
		return ModMod5
	case "all":
		return ModShift | ModLock | ModControl | ModMod1 | ModMod2 | ModMod3 | ModMod4 | ModMod5
	default:
		return 0
	}
}

// applyInterprets applies interpret statements to keys.
// This is called after parsing to set key properties based on matching interprets.
//
// In XKB, interpret statements primarily affect modifier and action keys.
// The "interpret Any + Any" catch-all applies to keys in the modifier map.
// We handle this by:
// 1. Matching specific keysyms from interpret statements
// 2. For keys producing modifier keysyms (Shift_L, Control_L, etc.),
//    applying the default interpret.repeat setting (typically False)
func (p *Parser) applyInterprets(keymap *Keymap) {
	// For each key, find matching interprets and apply their settings
	for _, key := range keymap.keys {
		// Collect all keysyms this key can produce
		keysyms := make(map[Keysym]bool)
		hasModifierKeysym := false
		for _, group := range key.groups {
			for _, level := range group.levels {
				for _, sym := range level.syms {
					if sym != KeyNoSymbol {
						keysyms[sym] = true
						if KeysymIsModifier(sym) {
							hasModifierKeysym = true
						}
					}
				}
			}
		}

		// Find the best matching interpret for this key
		// Priority: specific keysym match > no match
		var bestMatch *Interpret
		bestSpecificity := -1

		for _, interp := range keymap.interprets {
			// Only match specific keysyms, not the "Any" catch-all
			if interp.keysym == KeyNoSymbol {
				continue
			}

			if !keysyms[interp.keysym] {
				continue
			}

			// Specific keysym match
			specificity := 1

			// Modifier matching adds specificity
			if interp.modMatch != ModMatchNone {
				specificity += 2
			}

			// Take this match if it's better or equal (later wins)
			if specificity >= bestSpecificity {
				bestMatch = interp
				bestSpecificity = specificity
			}
		}

		// Apply the matching interpret's settings
		if bestMatch != nil {
			if bestMatch.repeat != nil {
				key.repeats = *bestMatch.repeat
			} else {
				key.repeats = p.interpretRepeatDefault
			}
		} else if hasModifierKeysym && p.interpretRepeatDefaultSet {
			// Keys producing modifier keysyms are affected by "interpret Any + Any"
			// which uses the default interpret.repeat setting.
			// Only apply if interpret.repeat was explicitly set in compat section,
			// otherwise preserve any explicit repeat setting from xkb_symbols.
			key.repeats = p.interpretRepeatDefault
		}
		// If no specific interpret matched and interpret.repeat wasn't set,
		// keep the key's repeat setting from xkb_symbols (or default true)
	}
}

// parseCompatIndicator parses: indicator "Name" { ... }
func (p *Parser) parseCompatIndicator(keymap *Keymap) error {
	name, err := p.expectString()
	if err != nil {
		return err
	}

	if err := p.expect(TokenLBrace); err != nil {
		return err
	}

	led, exists := keymap.leds[name]
	if !exists {
		led = &LED{name: name}
		keymap.leds[name] = led
	}

	// Parse indicator body
	for !p.check(TokenRBrace) && !p.check(TokenEOF) {
		if err := p.parseCompatIndicatorStatement(led, keymap); err != nil {
			p.addError(err)
			p.skipToSemicolonOrBrace()
		}
		if p.check(TokenSemicolon) {
			p.advance()
		}
	}

	if err := p.expect(TokenRBrace); err != nil {
		return err
	}

	return nil
}

// parseCompatIndicatorStatement parses a statement in an indicator definition.
func (p *Parser) parseCompatIndicatorStatement(led *LED, keymap *Keymap) error {
	// Handle negated flags like !allowExplicit
	if p.check(TokenBang) {
		p.advance()
		// Skip the negated flag
		p.skipToSemicolonOrBrace()
		return nil
	}

	ident, err := p.expectIdent()
	if err != nil {
		return err
	}

	switch ident {
	case "modifiers":
		if err := p.expect(TokenEquals); err != nil {
			return err
		}
		mods, err := p.parseModifierMask(keymap)
		if err != nil {
			return err
		}
		led.mods = mods
		return nil

	case "whichModState", "groups", "whichGroupState", "controls", "allowExplicit":
		// Skip these for now
		p.skipToSemicolonOrBrace()
		return nil

	default:
		// Skip unknown
		p.skipToSemicolonOrBrace()
		return nil
	}
}

// parseSymbols parses the xkb_symbols section.
func (p *Parser) parseSymbols(keymap *Keymap) error {
	// Optional section name
	if p.check(TokenString) {
		p.advance()
	}

	// Expect {
	if err := p.expect(TokenLBrace); err != nil {
		return err
	}

	// Parse statements until }
	for !p.check(TokenRBrace) && !p.check(TokenEOF) {
		if err := p.parseSymbolsStatement(keymap); err != nil {
			p.addError(err)
			p.skipToSemicolonOrBrace()
		}
		if p.check(TokenSemicolon) {
			p.advance()
		}
	}

	if err := p.expect(TokenRBrace); err != nil {
		return err
	}

	return p.expect(TokenSemicolon)
}

// parseSymbolsStatement parses a single statement in xkb_symbols.
func (p *Parser) parseSymbolsStatement(keymap *Keymap) error {
	// Handle key modifiers: replace, override, augment
	// These can appear before "key" in statements like "replace key <KEYNAME> { ... }"
	if p.check(TokenIdent) {
		switch p.current.Value {
		case "replace", "override", "augment":
			p.advance() // Skip the modifier
			// Now expect "key"
			if p.check(TokenIdent) && p.current.Value == "key" {
				p.advance()
				return p.parseKeyDefinition(keymap)
			}
			// Not followed by "key" - skip the statement with brace awareness
			p.skipStatementWithBraces()
			return nil
		}
	}

	// Check for key definition: key <NAME> { ... } or key.type[...] = ...
	if p.check(TokenIdent) && p.current.Value == "key" {
		p.advance()
		// Check for key.type (default type setting) vs key <NAME>
		if p.check(TokenDot) {
			// key.type[group1] = "TYPE" - skip default type settings
			p.skipStatementWithBraces()
			return nil
		}
		return p.parseKeyDefinition(keymap)
	}

	ident, err := p.expectIdent()
	if err != nil {
		return err
	}

	switch ident {
	case "name":
		return p.parseGroupName(keymap)
	case "group_offset":
		if err := p.expect(TokenEquals); err != nil { return err }
		num, err := p.expectNumber()
		if err != nil { return err }
		p.groupOffset = num
		return nil
	case "virtual_modifiers":
		return p.parseVirtualModifiers(keymap)
	case "modifier_map":
		return p.parseModifierMap(keymap)
	case "include":
		// Skip include statements (keymap should be self-contained)
		_, _ = p.expectString()
		return nil
	default:
		// Skip unknown statements with brace awareness
		p.skipStatementWithBraces()
		return nil
	}
}

// parseGroupName parses: name[GroupN] = "Name" or name[N] = "Name"
func (p *Parser) parseGroupName(keymap *Keymap) error {
	if err := p.expect(TokenLBracket); err != nil {
		return err
	}

	var group int
	if p.check(TokenNumber) {
		num, err := p.expectNumber()
		if err != nil { return err }
		group = num - 1 + p.groupOffset
	} else {
		groupIdent, err := p.expectIdent()
		if err != nil { return err }
		group, err = p.parseGroupIdent(groupIdent)
		if err != nil { return err }
		group += p.groupOffset
	}

	if err := p.expect(TokenRBracket); err != nil { return err }
	if err := p.expect(TokenEquals); err != nil { return err }

	name, err := p.expectString()
	if err != nil { return err }

	for len(keymap.groupNames) <= group {
		keymap.groupNames = append(keymap.groupNames, "")
	}
	keymap.groupNames[group] = name

	if group+1 > keymap.numGroups {
		keymap.numGroups = group + 1
	}

	return nil
}

// parseGroupIdent parses a group identifier like "Group1" or "group1".
func (p *Parser) parseGroupIdent(name string) (int, error) {
	lowerName := strings.ToLower(name)
	if !strings.HasPrefix(lowerName, "group") {
		return 0, fmt.Errorf("invalid group name: %s", name)
	}
	numStr := name[5:]
	num, err := strconv.Atoi(numStr)
	if err != nil {
		return 0, fmt.Errorf("invalid group number: %s", name)
	}
	return num - 1, nil // Convert 1-based to 0-based
}

// keysymAny is a special marker used during parsing to indicate that the existing
// keysym at this level should be preserved (the "any" keyword in XKB).
const keysymAny Keysym = 0xFFFFFFFF

// parseKeyDefinition parses: key <NAME> { ... }
func (p *Parser) parseKeyDefinition(keymap *Keymap) error {
	keycodeName, err := p.expectKeycode()
	if err != nil {
		return err
	}

	keycode, exists := keymap.keycodesByName[keycodeName]
	if !exists {
		// Create a keycode for unknown names (happens with aliases)
		keycode = Keycode(len(keymap.keycodesByName) + int(keymap.minKeycode))
		keymap.keycodeNames[keycode] = keycodeName
		keymap.keycodesByName[keycodeName] = keycode

		// Auto-expand range
		if keycode > keymap.maxKeycode {
			keymap.maxKeycode = keycode
		}
	}

	if err := p.expect(TokenLBrace); err != nil {
		return err
	}

	// Check if key already exists (for merging with existing definition)
	existingKey := keymap.keys[keycode]

	key := &Key{
		keycode: keycode,
		name:    keycodeName,
		repeats: true,
	}

	// If there's an existing key, preserve its repeat setting
	if existingKey != nil {
		key.repeats = existingKey.repeats
	}

	// Parse key body - can be simple [ syms ] or complex { type = ..., symbols[GroupN] = ... }
	// Also handles mixed form: { [ syms ], repeat = no }
	if err := p.parseKeyBody(key, keymap, existingKey); err != nil {
		return err
	}

	if err := p.expect(TokenRBrace); err != nil {
		return err
	}

	keymap.keys[keycode] = key
	return nil
}

// parseKeyBody parses the body of a complex key definition.
// existingKey is the previously defined key for the same keycode, used for merging.
func (p *Parser) parseKeyBody(key *Key, keymap *Keymap, existingKey *Key) error {
	defer func() {
		if len(key.groups) > keymap.numGroups {
			keymap.numGroups = len(key.groups)
		}
	}()
	var currentTypeName string
	groups := make(map[int][]Keysym)

	for !p.check(TokenRBrace) && !p.check(TokenEOF) {
		if p.check(TokenLBracket) {
			syms, err := p.parseSymbolList(keymap)
			if err != nil { return err }
			groups[p.groupOffset] = syms
		} else {
			ident, err := p.expectIdent()
			if err != nil {
				return err
			}

			switch ident {
			case "type":
				if p.match(TokenLBracket) {
					// type[GroupN] = "TypeName"
					_, _ = p.expectIdent() // group
					_ = p.expect(TokenRBracket)
				}
				_ = p.expect(TokenEquals)
				typeName, err := p.expectString()
				if err != nil {
					return err
				}
				currentTypeName = typeName

			case "symbols":
				if err := p.expect(TokenLBracket); err != nil { return err }
				var group int
				if p.check(TokenNumber) {
					num, err := p.expectNumber()
					if err != nil { return err }
					group = num - 1 + p.groupOffset
				} else {
					groupIdent, err := p.expectIdent()
					if err != nil { return err }
					group, err = p.parseGroupIdent(groupIdent)
					if err != nil { return err }
					group += p.groupOffset
				}
				if err := p.expect(TokenRBracket); err != nil { return err }
				if err := p.expect(TokenEquals); err != nil {
					return err
				}
				syms, err := p.parseSymbolList(keymap)
				if err != nil {
					return err
				}
				groups[group] = syms

			case "actions":
				// Skip action definitions for now
				p.skipToSemicolonOrBrace()

			case "repeat":
				if err := p.expect(TokenEquals); err != nil {
					return err
				}
				repeatIdent, err := p.expectIdent()
				if err != nil {
					return err
				}
				key.repeats = repeatIdent == "yes" || repeatIdent == "true" || repeatIdent == "True"

			case "vmods", "virtualMods":
				// Virtual modifier mapping
				p.skipToSemicolonOrBrace()

			default:
				// Skip unknown
				p.skipToSemicolonOrBrace()
			}
		}

		// Consume comma or semicolon between elements
		if p.match(TokenComma, TokenSemicolon) {
			continue
		}
	}

	// Build key groups from parsed data, merging with existing key if present
	maxGroup := 0
	for g := range groups {
		if g > maxGroup {
			maxGroup = g
		}
	}
	// Also consider existing key's groups
	if existingKey != nil && len(existingKey.groups) > maxGroup+1 {
		maxGroup = len(existingKey.groups) - 1
	}

	key.groups = make([]KeyGroup, maxGroup+1)
	for g := 0; g <= maxGroup; g++ {
		syms := groups[g]
		var keyType *KeyType
		if currentTypeName != "" {
			keyType = keymap.types[currentTypeName]
		}

		// Merge with existing key if present
		if existingKey != nil && g < len(existingKey.groups) {
			existingGroup := existingKey.groups[g]

			// If we don't have new symbols for this group, use existing
			if syms == nil {
				key.groups[g] = existingGroup
				continue
			}

			// Merge symbols: replace keysymAny or KeyNoSymbol with existing symbol
			mergedSyms := make([]Keysym, len(syms))
			copy(mergedSyms, syms)
			for i, sym := range mergedSyms {
				if sym == keysymAny || sym == KeyNoSymbol {
					// Get existing symbol at this level
					if i < len(existingGroup.levels) && len(existingGroup.levels[i].syms) > 0 {
						mergedSyms[i] = existingGroup.levels[i].syms[0]
					} else {
						mergedSyms[i] = KeyNoSymbol
					}
				}
			}
			syms = mergedSyms

			// Use existing type if we don't have a new one
			if keyType == nil {
				keyType = existingGroup.keyType
				// Upgrade type if the merged symbols array requires more levels
				if keyType != nil && len(syms) > keyType.numLevels {
					keyType = p.guessKeyType(keymap, len(syms))
				}
			}
		}

		if keyType == nil || len(syms) > keyType.numLevels {
			keyType = p.guessKeyType(keymap, len(syms))
		}
		key.groups[g] = KeyGroup{
			keyType: keyType,
			levels:  p.symsToLevels(syms),
		}
	}

	return nil
}

// parseSymbolList parses: [ sym1, sym2, ... ]
func (p *Parser) parseSymbolList(keymap *Keymap) ([]Keysym, error) {
	if err := p.expect(TokenLBracket); err != nil {
		return nil, err
	}

	var syms []Keysym
	for !p.check(TokenRBracket) && !p.check(TokenEOF) {
		sym, err := p.parseKeysym()
		if err != nil {
			return nil, err
		}
		syms = append(syms, sym)

		if !p.match(TokenComma) {
			break
		}
	}

	if err := p.expect(TokenRBracket); err != nil {
		return nil, err
	}

	return syms, nil
}

// parseKeysym parses a single keysym (identifier or number).
func (p *Parser) parseKeysym() (Keysym, error) {
	if p.check(TokenNumber) {
		numStr := p.current.Value
		p.advance()

		// Single digit numbers are keysym names (0-9), not hex values
		// e.g., "1" means XK_1 (0x31), not Keysym(1)
		if len(numStr) == 1 && numStr[0] >= '0' && numStr[0] <= '9' {
			return Keysym(numStr[0]), nil
		}

		// Hex numbers are literal keysym values
		num, err := p.parseNumber(numStr)
		if err != nil {
			return 0, err
		}
		return Keysym(num), nil
	}

	if p.check(TokenIdent) {
		name := p.current.Value
		p.advance()

		// Special handling for "any" keyword - means preserve existing symbol
		if name == "any" {
			return keysymAny, nil
		}

		// Look up keysym by name
		ks := KeysymFromName(name, KeysymNameNoFlags)
		if ks == KeyNoSymbol {
			// Try as a single character
			if len(name) == 1 {
				return Keysym(name[0]), nil
			}
		}
		return ks, nil
	}

	return 0, p.errorf("expected keysym")
}

// guessKeyType guesses the appropriate key type based on number of symbols.
func (p *Parser) guessKeyType(keymap *Keymap, numSyms int) *KeyType {
	switch numSyms {
	case 1:
		if t, ok := keymap.types["ONE_LEVEL"]; ok {
			return t
		}
	case 2:
		if t, ok := keymap.types["TWO_LEVEL"]; ok {
			return t
		}
		if t, ok := keymap.types["ALPHABETIC"]; ok {
			return t
		}
	case 4:
		if t, ok := keymap.types["FOUR_LEVEL"]; ok {
			return t
		}
	}

	// Fallback: create a simple type
	return &KeyType{
		name:      "default",
		numLevels: numSyms,
	}
}

// symsToLevels converts a slice of keysyms to KeyLevel slices.
func (p *Parser) symsToLevels(syms []Keysym) []KeyLevel {
	levels := make([]KeyLevel, len(syms))
	for i, sym := range syms {
		levels[i] = KeyLevel{syms: []Keysym{sym}}
	}
	return levels
}

// parseModifierMap parses: modifier_map ModName { <KEY1>, <KEY2>, ... }
func (p *Parser) parseModifierMap(keymap *Keymap) error {
	modName, err := p.expectIdent()
	if err != nil {
		return err
	}

	modMask := p.modifierNameToMask(modName, keymap)

	if err := p.expect(TokenLBrace); err != nil {
		return err
	}

	for !p.check(TokenRBrace) && !p.check(TokenEOF) {
		if p.check(TokenKeycode) {
			keycodeName := p.current.Value
			p.advance()

			if keycode, ok := keymap.keycodesByName[keycodeName]; ok {
				if key, ok := keymap.keys[keycode]; ok {
					key.vmodmap |= modMask
				}
			}
		} else if p.check(TokenIdent) {
			// Skip keysym names
			p.advance()
		}

		if !p.match(TokenComma) {
			break
		}
	}

	if err := p.expect(TokenRBrace); err != nil {
		return err
	}

	return nil
}
