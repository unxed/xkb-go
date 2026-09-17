package xkb

import "context"

// This file provides test helpers for building keymaps programmatically.
// These are exported for use in tests of packages that depend on xkb-go.

// TestKeymap creates a minimal US QWERTY [Keymap] for testing.
//
// This keymap includes basic alphanumeric keys and modifiers.
// It is useful for unit tests that need a valid keymap without
// loading from system files.
func TestKeymap() *Keymap {
	ctx := NewContext(context.Background(), ContextNoFlags)

	// Create key types
	oneLevel := &KeyType{
		name:      "ONE_LEVEL",
		mods:      0,
		numLevels: 1,
		entries:   []KeyTypeEntry{{mods: 0, level: 0}},
	}

	twoLevel := &KeyType{
		name:      "TWO_LEVEL",
		mods:      ModShift,
		numLevels: 2,
		entries: []KeyTypeEntry{
			{mods: 0, level: 0},
			{mods: ModShift, level: 1},
		},
	}

	alphabetic := &KeyType{
		name:      "ALPHABETIC",
		mods:      ModShift | ModLock,
		numLevels: 2,
		entries: []KeyTypeEntry{
			{mods: 0, level: 0},
			{mods: ModShift, level: 1},
			{mods: ModLock, level: 1},
			{mods: ModShift | ModLock, level: 0}, // Shift cancels Caps Lock
		},
	}

	keypad := &KeyType{
		name:      "KEYPAD",
		mods:      ModMod2, // Num Lock
		numLevels: 2,
		entries: []KeyTypeEntry{
			{mods: 0, level: 0},
			{mods: ModMod2, level: 1},
		},
	}

	km := &Keymap{
		ctx:            ctx,
		keycodeNames:   make(map[Keycode]string),
		keycodesByName: make(map[string]Keycode),
		minKeycode:     8,
		maxKeycode:     255,
		types: map[string]*KeyType{
			"ONE_LEVEL":  oneLevel,
			"TWO_LEVEL":  twoLevel,
			"ALPHABETIC": alphabetic,
			"KEYPAD":     keypad,
		},
		typesList:   []*KeyType{oneLevel, twoLevel, alphabetic, keypad},
		keys:        make(map[Keycode]*Key),
		modNames:    [8]string{"Shift", "Lock", "Control", "Mod1", "Mod2", "Mod3", "Mod4", "Mod5"},
		virtualMods: map[string]ModMask{"Alt": ModMod1, "NumLock": ModMod2, "Super": ModMod4},
		leds: map[string]*LED{
			"Caps Lock": {name: "Caps Lock", mods: ModLock},
			"Num Lock":  {name: "Num Lock", mods: ModMod2},
		},
		groupNames: []string{"English (US)"},
		numGroups:  1,
	}

	// Add letter keys (A-Z)
	// Row: QWERTYUIOP - keycodes 24-33
	// Row: ASDFGHJKL - keycodes 38-46
	// Row: ZXCVBNM - keycodes 52-58
	letterKeys := map[Keycode][2]Keysym{
		// Top row
		24: {'q', 'Q'}, 25: {'w', 'W'}, 26: {'e', 'E'}, 27: {'r', 'R'}, 28: {'t', 'T'},
		29: {'y', 'Y'}, 30: {'u', 'U'}, 31: {'i', 'I'}, 32: {'o', 'O'}, 33: {'p', 'P'},
		// Home row
		38: {'a', 'A'}, 39: {'s', 'S'}, 40: {'d', 'D'}, 41: {'f', 'F'}, 42: {'g', 'G'},
		43: {'h', 'H'}, 44: {'j', 'J'}, 45: {'k', 'K'}, 46: {'l', 'L'},
		// Bottom row
		52: {'z', 'Z'}, 53: {'x', 'X'}, 54: {'c', 'C'}, 55: {'v', 'V'}, 56: {'b', 'B'},
		57: {'n', 'N'}, 58: {'m', 'M'},
	}

	letterNames := map[Keycode]string{
		24: "AD01", 25: "AD02", 26: "AD03", 27: "AD04", 28: "AD05",
		29: "AD06", 30: "AD07", 31: "AD08", 32: "AD09", 33: "AD10",
		38: "AC01", 39: "AC02", 40: "AC03", 41: "AC04", 42: "AC05",
		43: "AC06", 44: "AC07", 45: "AC08", 46: "AC09",
		52: "AB01", 53: "AB02", 54: "AB03", 55: "AB04", 56: "AB05",
		57: "AB06", 58: "AB07",
	}

	for kc, syms := range letterKeys {
		name := letterNames[kc]
		km.keycodeNames[kc] = name
		km.keycodesByName[name] = kc
		km.keys[kc] = &Key{
			keycode: kc,
			name:    name,
			groups: []KeyGroup{{
				keyType: alphabetic,
				levels: []KeyLevel{
					{syms: []Keysym{syms[0]}},
					{syms: []Keysym{syms[1]}},
				},
			}},
			repeats: true,
		}
	}

	// Add number keys (1-0)
	numberKeys := map[Keycode][2]Keysym{
		10: {'1', '!'}, 11: {'2', '@'}, 12: {'3', '#'}, 13: {'4', '$'}, 14: {'5', '%'},
		15: {'6', '^'}, 16: {'7', '&'}, 17: {'8', '*'}, 18: {'9', '('}, 19: {'0', ')'},
	}

	for kc, syms := range numberKeys {
		name := "AE0" + string(rune('0'+kc-10))
		km.keycodeNames[kc] = name
		km.keycodesByName[name] = kc
		km.keys[kc] = &Key{
			keycode: kc,
			name:    name,
			groups: []KeyGroup{{
				keyType: twoLevel,
				levels: []KeyLevel{
					{syms: []Keysym{syms[0]}},
					{syms: []Keysym{syms[1]}},
				},
			}},
			repeats: true,
		}
	}

	// Add special keys
	specialKeys := map[Keycode]struct {
		name    string
		sym     Keysym
		keyType *KeyType
		repeats bool
	}{
		9:   {"ESC", KeyEscape, oneLevel, false},
		22:  {"BKSP", KeyBackSpace, oneLevel, true},
		23:  {"TAB", KeyTab, oneLevel, true},
		36:  {"RTRN", KeyReturn, oneLevel, false},
		65:  {"SPCE", Keysym(' '), oneLevel, true},
		50:  {"LFSH", KeyShiftL, oneLevel, false},
		62:  {"RTSH", KeyShiftR, oneLevel, false},
		37:  {"LCTL", KeyControlL, oneLevel, false},
		105: {"RCTL", KeyControlR, oneLevel, false},
		64:  {"LALT", KeyAltL, oneLevel, false},
		108: {"RALT", KeyAltR, oneLevel, false},
		133: {"LWIN", KeySuperL, oneLevel, false},
		66:  {"CAPS", KeyCapsLock, oneLevel, false},
	}

	for kc, info := range specialKeys {
		km.keycodeNames[kc] = info.name
		km.keycodesByName[info.name] = kc
		km.keys[kc] = &Key{
			keycode: kc,
			name:    info.name,
			groups: []KeyGroup{{
				keyType: info.keyType,
				levels:  []KeyLevel{{syms: []Keysym{info.sym}}},
			}},
			repeats: info.repeats,
		}
	}

	return km
}

// TestComposeTable creates a minimal [ComposeTable] for testing.
//
// Includes common dead key sequences like dead_acute + a = á.
// Useful for unit tests that need compose functionality without
// loading system compose files.
func TestComposeTable() *ComposeTable {
	// Build a simple trie for testing
	root := &composeNode{children: make(map[Keysym]*composeNode)}

	// dead_acute + a = á
	addComposeSequence(root, []Keysym{KeyDeadAcute, 'a'}, 0x00e1, "á")
	addComposeSequence(root, []Keysym{KeyDeadAcute, 'A'}, 0x00c1, "Á")
	addComposeSequence(root, []Keysym{KeyDeadAcute, 'e'}, 0x00e9, "é")
	addComposeSequence(root, []Keysym{KeyDeadAcute, 'E'}, 0x00c9, "É")

	// dead_grave + a = à
	addComposeSequence(root, []Keysym{KeyDeadGrave, 'a'}, 0x00e0, "à")
	addComposeSequence(root, []Keysym{KeyDeadGrave, 'A'}, 0x00c0, "À")

	// dead_diaeresis + a = ä
	addComposeSequence(root, []Keysym{KeyDeadDiaeresis, 'a'}, 0x00e4, "ä")
	addComposeSequence(root, []Keysym{KeyDeadDiaeresis, 'A'}, 0x00c4, "Ä")
	addComposeSequence(root, []Keysym{KeyDeadDiaeresis, 'u'}, 0x00fc, "ü")
	addComposeSequence(root, []Keysym{KeyDeadDiaeresis, 'U'}, 0x00dc, "Ü")

	// Multi_key sequences
	addComposeSequence(root, []Keysym{KeyMultiKey, 'a', 'e'}, 0x00e6, "æ")
	addComposeSequence(root, []Keysym{KeyMultiKey, 'A', 'E'}, 0x00c6, "Æ")
	addComposeSequence(root, []Keysym{KeyMultiKey, 'c', ','}, 0x00e7, "ç")
	addComposeSequence(root, []Keysym{KeyMultiKey, 'C', ','}, 0x00c7, "Ç")

	return &ComposeTable{
		locale: "en_US.UTF-8",
		root:   root,
	}
}

func addComposeSequence(root *composeNode, sequence []Keysym, resultKeysym Keysym, resultUTF8 string) {
	node := root
	for i, sym := range sequence {
		if node.children == nil {
			node.children = make(map[Keysym]*composeNode)
		}
		next, ok := node.children[sym]
		if !ok {
			next = &composeNode{}
			node.children[sym] = next
		}
		if i == len(sequence)-1 {
			next.result = &composeResult{keysym: resultKeysym, utf8: resultUTF8}
		}
		node = next
	}
}
