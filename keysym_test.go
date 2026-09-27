package xkb

import (
	"strings"
	"testing"
)

func TestKeysymToUTF32(t *testing.T) {
	tests := []struct {
		name   string
		keysym Keysym
		want   rune
	}{
		{"lowercase a", Keysym('a'), 'a'},
		{"uppercase A", Keysym('A'), 'A'},
		{"space", Keysym(' '), ' '},
		{"digit 0", Keysym('0'), '0'},
		{"Latin-1 ä", Keysym(0xe4), 'ä'},
		{"Unicode keysym", Keysym(0x01000041), 'A'},
		{"Unicode emoji", Keysym(0x0100263a), '\u263a'}, // ☺
		{"Return (no char)", KeyReturn, 0},
		{"Shift_L (no char)", KeyShiftL, 0},
		{"NoSymbol", KeyNoSymbol, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := KeysymToUTF32(tt.keysym)
			if got != tt.want {
				t.Errorf("KeysymToUTF32(%#x) = %#x, want %#x", tt.keysym, got, tt.want)
			}
		})
	}
}

func TestKeysymToUTF8(t *testing.T) {
	tests := []struct {
		name   string
		keysym Keysym
		want   string
	}{
		{"lowercase a", Keysym('a'), "a"},
		{"uppercase A", Keysym('A'), "A"},
		{"Latin-1 ä", Keysym(0xe4), "ä"},
		{"Unicode keysym for ü", Keysym(0x010000fc), "ü"},
		{"Return (empty)", KeyReturn, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := KeysymToUTF8(tt.keysym)
			if got != tt.want {
				t.Errorf("KeysymToUTF8(%#x) = %q, want %q", tt.keysym, got, tt.want)
			}
		})
	}
}

func TestUTF32ToKeysym(t *testing.T) {
	tests := []struct {
		name string
		r    rune
		want Keysym
	}{
		{"lowercase a", 'a', Keysym('a')},
		{"uppercase A", 'A', Keysym('A')},
		{"space", ' ', Keysym(' ')},
		{"Latin-1 ä", 'ä', Keysym(0xe4)},
		{"Greek alpha (has specific keysym)", 'α', KeyGreekAlpha}, // 0x07e1
		{"Emoji (Unicode keysym)", '😀', Keysym(0x0101f600)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := UTF32ToKeysym(tt.r)
			if got != tt.want {
				t.Errorf("UTF32ToKeysym(%#x) = %#x, want %#x", tt.r, got, tt.want)
			}
		})
	}
}

func TestKeysymGetName(t *testing.T) {
	tests := []struct {
		keysym Keysym
		want   string
	}{
		{KeyReturn, "Return"},
		{KeyEscape, "Escape"},
		{KeyShiftL, "Shift_L"},
		{KeyShiftR, "Shift_R"},
		{KeyBackSpace, "BackSpace"},
		{KeyTab, "Tab"},
		{KeyF1, "F1"},
		{KeyF12, "F12"},
		{KeyDeadAcute, "dead_acute"},
		{KeyMultiKey, "Multi_key"},
		{Keysym('a'), "a"},
		{Keysym('A'), "A"},
		{Keysym(' '), "space"},
		{Keysym('+'), "plus"},
		{Keysym('-'), "minus"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := KeysymGetName(tt.keysym)
			if got != tt.want {
				t.Errorf("KeysymGetName(%#x) = %q, want %q", tt.keysym, got, tt.want)
			}
		})
	}
}

func TestKeysymFromName(t *testing.T) {
	tests := []struct {
		name string
		want Keysym
	}{
		{"Return", KeyReturn},
		{"Escape", KeyEscape},
		{"Shift_L", KeyShiftL},
		{"BackSpace", KeyBackSpace},
		{"F1", KeyF1},
		{"dead_acute", KeyDeadAcute},
		{"Multi_key", KeyMultiKey},
		{"a", Keysym('a')},
		{"A", Keysym('A')},
		{"space", Keysym(' ')},
		{"plus", Keysym('+')},
		{"nonexistent", KeyNoSymbol},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := KeysymFromName(tt.name, KeysymNameNoFlags)
			if got != tt.want {
				t.Errorf("KeysymFromName(%q) = %#x, want %#x", tt.name, got, tt.want)
			}
		})
	}
}

func TestKeysymRoundTrip(t *testing.T) {
	// Test that name -> keysym -> name works
	names := []string{
		"Return", "Escape", "Shift_L", "Control_L", "Alt_L",
		"F1", "F12", "BackSpace", "Tab", "Delete",
		"a", "z", "A", "Z", "0", "9",
		"space", "plus", "minus", "equal",
	}

	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			ks := KeysymFromName(name, KeysymNameNoFlags)
			if ks == KeyNoSymbol {
				t.Fatalf("KeysymFromName(%q) returned NoSymbol", name)
			}

			gotName := KeysymGetName(ks)
			if gotName != name {
				t.Errorf("Round trip: %q -> %#x -> %q", name, ks, gotName)
			}
		})
	}
}

func TestKeysymIsModifier(t *testing.T) {
	modifiers := []Keysym{
		KeyShiftL, KeyShiftR, KeyControlL, KeyControlR,
		KeyAltL, KeyAltR, KeySuperL, KeySuperR,
		KeyMetaL, KeyMetaR, KeyHyperL, KeyHyperR,
		KeyCapsLock, KeyNumLock, KeyScrollLock,
	}

	for _, ks := range modifiers {
		name := KeysymGetName(ks)
		if !KeysymIsModifier(ks) {
			t.Errorf("KeysymIsModifier(%s) = false, want true", name)
		}
	}

	nonModifiers := []Keysym{
		Keysym('a'), KeyReturn, KeyF1, KeyBackSpace,
	}

	for _, ks := range nonModifiers {
		name := KeysymGetName(ks)
		if KeysymIsModifier(ks) {
			t.Errorf("KeysymIsModifier(%s) = true, want false", name)
		}
	}
}

func TestKeysymIsKeypad(t *testing.T) {
	keypadKeys := []Keysym{
		KeyKP0, KeyKP1, KeyKP9, KeyKPEnter, KeyKPAdd, KeyKPSubtract,
	}

	for _, ks := range keypadKeys {
		name := KeysymGetName(ks)
		if !KeysymIsKeypad(ks) {
			t.Errorf("KeysymIsKeypad(%s) = false, want true", name)
		}
	}

	nonKeypadKeys := []Keysym{
		Keysym('0'), KeyReturn, KeyF1,
	}

	for _, ks := range nonKeypadKeys {
		if KeysymIsKeypad(ks) {
			t.Errorf("KeysymIsKeypad(%#x) = true, want false", ks)
		}
	}
}

func TestKeysymIsFunctionKey(t *testing.T) {
	funcKeys := []Keysym{KeyF1, KeyF2, KeyF10, KeyF11, KeyF12}
	for _, ks := range funcKeys {
		if !KeysymIsFunctionKey(ks) {
			t.Errorf("KeysymIsFunctionKey(%s) = false, want true", KeysymGetName(ks))
		}
	}

	nonFuncKeys := []Keysym{Keysym('a'), KeyReturn, KeyShiftL}
	for _, ks := range nonFuncKeys {
		if KeysymIsFunctionKey(ks) {
			t.Errorf("KeysymIsFunctionKey(%#x) = true, want false", ks)
		}
	}
}

func TestKeysymGetNameUnicode(t *testing.T) {
	// Test Unicode keysym path (not in table)
	unicodeKeysym := Keysym(0x01000041) // Unicode 'A'
	name := KeysymGetName(unicodeKeysym)
	if name != "A" {
		t.Errorf("KeysymGetName(0x01000041) = %q, want %q", name, "A")
	}

	// Test unknown keysym
	unknownKeysym := Keysym(0x12345678)
	name = KeysymGetName(unknownKeysym)
	if name != "" {
		t.Errorf("KeysymGetName(0x12345678) = %q, want empty", name)
	}
}

func TestKeysymConstants(t *testing.T) {
	// Verify some well-known keysym values
	tests := []struct {
		name   string
		keysym Keysym
		value  uint32
	}{
		{"BackSpace", KeyBackSpace, 0xff08},
		{"Tab", KeyTab, 0xff09},
		{"Return", KeyReturn, 0xff0d},
		{"Escape", KeyEscape, 0xff1b},
		{"Shift_L", KeyShiftL, 0xffe1},
		{"Control_L", KeyControlL, 0xffe3},
		{"Alt_L", KeyAltL, 0xffe9},
		{"Super_L", KeySuperL, 0xffeb},
		{"F1", KeyF1, 0xffbe},
		{"dead_acute", KeyDeadAcute, 0xfe51},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if uint32(tt.keysym) != tt.value {
				t.Errorf("%s = %#x, want %#x", tt.name, tt.keysym, tt.value)
			}
		})
	}
}

func TestKeysymEdgeCases(t *testing.T) {
	t.Run("boundary keysyms", func(t *testing.T) {
		boundaries := []Keysym{
			0,          // NoSymbol
			0x0020,     // space (lowest printable)
			0x007e,     // tilde (highest ASCII printable)
			0x007f,     // DEL (not printable)
			0x00a0,     // NBSP (start of Latin-1 supplement)
			0x00ff,     // ÿ (end of Latin-1)
			0x0100,     // Start of Latin Extended
			0xff08,     // BackSpace
			0xffff,     // End of legacy keysyms
			0x01000000, // Start of Unicode keysyms
			0x01000041, // Unicode 'A'
			0x0110ffff, // End of valid Unicode keysyms
			0x01110000, // Invalid (beyond Unicode)
			0xffffffff, // Max uint32
		}

		for _, ks := range boundaries {
			// Should not panic
			name := KeysymGetName(ks)
			utf32 := KeysymToUTF32(ks)
			utf8 := KeysymToUTF8(ks)
			_ = name
			_ = utf32
			_ = utf8
		}
	})

	t.Run("unicode keysym range", func(t *testing.T) {
		tests := []struct {
			keysym Keysym
			want   rune
		}{
			{0x01000041, 'A'},
			{0x010000e4, 'ä'},
			{0x01002603, '☃'},
			{0x0101f600, 0x1f600},
			{0x0110ffff, 0x10ffff},
		}

		for _, tt := range tests {
			got := KeysymToUTF32(tt.keysym)
			if got != tt.want {
				t.Errorf("KeysymToUTF32(%#x) = %#x, want %#x", tt.keysym, got, tt.want)
			}
		}
	})

	t.Run("name lookup edge cases", func(t *testing.T) {
		names := []string{
			"",
			"a",
			"A",
			"space",
			"SPACE",
			"dead_acute",
			"Dead_Acute",
			strings.Repeat("x", 100),
			"non_existent_keysym",
			"0x0020",
			"Return\n",
			" space ",
		}

		for _, name := range names {
			ks := KeysymFromName(name, KeysymNameNoFlags)
			ksCaseInsensitive := KeysymFromName(name, KeysymNameCaseInsensitive)
			_ = ks
			_ = ksCaseInsensitive
		}
	})
}
