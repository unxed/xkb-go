package xkb

import (
	"fmt"
	"strings"
)

// Keymap is an immutable compiled keyboard mapping.
//
// It contains all information about keys, layouts, types, and modifiers.
// Create a Keymap using [Context.NewKeymapFromString], [Context.NewKeymapFromFile],
// or [Context.NewKeymapFromNames].
//
// Keymap is safe to share across goroutines after creation.
// Use [Keymap.NewState] to create a mutable [State] for key translation.
type Keymap struct {
	ctx *Context

	// Keycode ↔ name mapping
	keycodeNames   map[Keycode]string
	keycodesByName map[string]Keycode
	minKeycode     Keycode
	maxKeycode     Keycode

	// Key type definitions
	types     map[string]*KeyType
	typesList []*KeyType

	// Per-key information
	keys map[Keycode]*Key

	// Modifier definitions
	modNames    [8]string          // Real modifier names (Shift, Lock, Control, Mod1-5)
	virtualMods map[string]ModMask // Virtual modifier → real modifier mask

	// LED/indicator definitions
	leds map[string]*LED

	// Group (layout) names
	groupNames []string
	numGroups  int

	// Interpret statements from compat section
	interprets []*Interpret
}

// Interpret represents an interpret statement from xkb_compat.
//
// It maps keysyms (optionally with modifier conditions) to actions and properties.
// This is an internal type used during keymap compilation.
type Interpret struct {
	keysym   Keysym   // The keysym to match (KeyNoSymbol means "Any")
	modMatch ModMatch // How to match modifiers
	mods     ModMask  // Modifier mask for matching
	repeat   *bool    // nil means use default, non-nil overrides
	// action is not stored as we don't implement actions yet
}

// ModMatch specifies how to match modifiers in an [Interpret] statement.
type ModMatch int

const (
	// ModMatchNone means no modifier matching is performed.
	ModMatchNone ModMatch = iota
	// ModMatchAnyOfOrNone matches if any of the mods are active, or if none are.
	ModMatchAnyOfOrNone
	// ModMatchAnyOf matches if any of the specified mods are active.
	ModMatchAnyOf
	// ModMatchNoneOf matches if none of the specified mods are active.
	ModMatchNoneOf
	// ModMatchAllOf matches if all of the specified mods are active.
	ModMatchAllOf
	// ModMatchExactly matches if exactly these mods are active (no more, no less).
	ModMatchExactly
)

// KeyType defines how modifiers affect the shift level of a key.
//
// For example, the "ALPHABETIC" type considers Shift and Lock modifiers,
// mapping them to different levels (lowercase, uppercase).
type KeyType struct {
	name      string
	mods      ModMask // Modifiers this type considers
	numLevels int
	entries   []KeyTypeEntry
}

// KeyTypeEntry maps a modifier combination to a level within a [KeyType].
type KeyTypeEntry struct {
	mods     ModMask // Modifier combination
	level    Level   // Resulting level
	preserve ModMask // Modifiers to preserve (not consume)
}

// Key holds per-key information including symbols for each group and level.
type Key struct {
	keycode Keycode
	name    string
	groups  []KeyGroup
	repeats bool
	vmodmap ModMask // Virtual modifiers this key activates
}

// KeyGroup holds key symbols for one group (layout) of a [Key].
type KeyGroup struct {
	keyType *KeyType
	levels  []KeyLevel
}

// KeyLevel holds keysyms for one shift level of a [KeyGroup].
type KeyLevel struct {
	syms []Keysym // Usually 1, rarely more (e.g., for key aliases)
}

// LED represents a keyboard LED indicator (e.g., Caps Lock, Num Lock).
//
// Use [State.LEDNameIsActive] to check if an LED should be lit.
type LED struct {
	name  string
	index int     // Hardware LED index (1-based from keycodes section)
	mods  ModMask // Modifiers that activate this LED
	group Group   // Group that activates this LED
}

// MinKeycode returns the minimum [Keycode] in the keymap.
func (km *Keymap) MinKeycode() Keycode {
	return km.minKeycode
}

// MaxKeycode returns the maximum [Keycode] in the keymap.
func (km *Keymap) MaxKeycode() Keycode {
	return km.maxKeycode
}

// KeyGetName returns the symbolic name for a keycode (e.g., "AD01" for Q).
//
// Returns empty string if keycode is not found.
// See also [Keymap.KeyByName] for the reverse lookup.
func (km *Keymap) KeyGetName(keycode Keycode) string {
	return km.keycodeNames[keycode]
}

// KeyByName returns the [Keycode] for a symbolic name.
//
// Returns 0 if name is not found.
// See also [Keymap.KeyGetName] for the reverse lookup.
func (km *Keymap) KeyByName(name string) Keycode {
	return km.keycodesByName[name]
}

// NumGroups returns the number of groups (layouts) in the keymap.
//
// Most keymaps have 1-4 groups.
func (km *Keymap) NumGroups() int {
	return km.numGroups
}

// GroupName returns the name of a [Group] (layout).
//
// Returns empty string if group index is out of range.
func (km *Keymap) GroupName(group Group) string {
	if int(group) >= len(km.groupNames) {
		return ""
	}
	return km.groupNames[group]
}

// NumTypes returns the number of [KeyType] definitions in the keymap.
func (km *Keymap) NumTypes() int {
	return len(km.typesList)
}

// ModGetIndex returns the index of a modifier by name.
//
// Returns -1 if not found. Works for both real modifiers (Shift, Lock,
// Control, Mod1-5) and virtual modifiers (Alt, Super, etc.).
//
// Use with [State.ModIndexIsActive] to check modifier state.
func (km *Keymap) ModGetIndex(name string) int {
	// Check real modifiers first
	for i, n := range km.modNames {
		if n == name {
			return i
		}
	}
	// Virtual modifiers are indexed after real modifiers
	// but we return the real modifier index they map to
	if mask, ok := km.virtualMods[name]; ok {
		// Find the first set bit
		for i := 0; i < 8; i++ {
			if mask&(1<<i) != 0 {
				return i
			}
		}
	}
	return -1
}

// NumLEDs returns the number of [LED] indicators in the keymap.
func (km *Keymap) NumLEDs() int {
	return len(km.leds)
}

// LEDGetIndex returns the index of an [LED] by name.
//
// Returns -1 if not found. See also [Keymap.LEDGetName].
func (km *Keymap) LEDGetIndex(name string) int {
	i := 0
	for n := range km.leds {
		if n == name {
			return i
		}
		i++
	}
	return -1
}

// LEDGetName returns the name of an [LED] by index.
//
// Returns empty string if index is out of range. See also [Keymap.LEDGetIndex].
func (km *Keymap) LEDGetName(index int) string {
	i := 0
	for n := range km.leds {
		if i == index {
			return n
		}
		i++
	}
	return ""
}

// KeyRepeats returns whether a key should repeat when held.
//
// Keys like letters and numbers repeat, while modifiers do not.
func (km *Keymap) KeyRepeats(keycode Keycode) bool {
	if key, ok := km.keys[keycode]; ok {
		return key.repeats
	}
	return false
}

// NewState creates a new keyboard [State] for this keymap.
//
// Each keyboard device should have its own State instance.
// The State tracks modifier and group state for key translation.
func (km *Keymap) NewState() *State {
	return &State{
		keymap: km,
	}
}

// Context returns the [Context] this keymap was created from.
func (km *Keymap) Context() *Context {
	return km.ctx
}

// GetAsString serializes the keymap to XKB text format.
//
// The format parameter must be [KeymapFormatTextV1].
// The returned string can be passed to [Context.NewKeymapFromString].
func (km *Keymap) GetAsString(format KeymapFormat) (string, error) {
	if format != KeymapFormatTextV1 {
		return "", ErrUnsupportedFormat
	}

	var b strings.Builder

	b.WriteString("xkb_keymap {\n")

	// xkb_keycodes section
	km.writeKeycodes(&b)

	// xkb_types section
	km.writeTypes(&b)

	// xkb_compat section
	km.writeCompat(&b)

	// xkb_symbols section
	km.writeSymbols(&b)

	b.WriteString("};\n")

	return b.String(), nil
}

func (km *Keymap) writeKeycodes(b *strings.Builder) {
	b.WriteString("xkb_keycodes {\n")
	fmt.Fprintf(b, "\tminimum = %d;\n", km.minKeycode)
	fmt.Fprintf(b, "\tmaximum = %d;\n", km.maxKeycode)

	// Output keycode names sorted by keycode
	for kc := km.minKeycode; kc <= km.maxKeycode; kc++ {
		if name, ok := km.keycodeNames[kc]; ok {
			fmt.Fprintf(b, "\t<%s> = %d;\n", name, kc)
		}
	}

	// Output indicators (LEDs)
	for name, led := range km.leds {
		if led.index > 0 {
			fmt.Fprintf(b, "\tindicator %d = \"%s\";\n", led.index, name)
		}
	}

	b.WriteString("};\n\n")
}

func (km *Keymap) writeTypes(b *strings.Builder) {
	b.WriteString("xkb_types {\n")

	// Output virtual modifiers
	if len(km.virtualMods) > 0 {
		b.WriteString("\tvirtual_modifiers ")
		first := true
		for name := range km.virtualMods {
			if !first {
				b.WriteString(",")
			}
			b.WriteString(name)
			first = false
		}
		b.WriteString(";\n\n")
	}

	// Output type definitions
	for _, kt := range km.typesList {
		fmt.Fprintf(b, "\ttype \"%s\" {\n", kt.name)

		// Output modifiers
		if kt.mods != 0 {
			fmt.Fprintf(b, "\t\tmodifiers= %s;\n", km.modMaskToString(kt.mods))
		} else {
			b.WriteString("\t\tmodifiers= none;\n")
		}

		// Output map entries (convert 0-based internal levels to 1-based XKB format)
		for _, entry := range kt.entries {
			if entry.mods != 0 {
				fmt.Fprintf(b, "\t\tmap[%s]= %d;\n", km.modMaskToString(entry.mods), entry.level+1)
				if entry.preserve != 0 {
					fmt.Fprintf(b, "\t\tpreserve[%s]= %s;\n", km.modMaskToString(entry.mods), km.modMaskToString(entry.preserve))
				}
			}
		}

		b.WriteString("\t};\n")
	}

	b.WriteString("};\n\n")
}

func (km *Keymap) writeCompat(b *strings.Builder) {
	b.WriteString("xkb_compatibility {\n")

	// Output virtual modifiers (same as types section)
	if len(km.virtualMods) > 0 {
		b.WriteString("\tvirtual_modifiers ")
		first := true
		for name := range km.virtualMods {
			if !first {
				b.WriteString(",")
			}
			b.WriteString(name)
			first = false
		}
		b.WriteString(";\n\n")
	}

	// Output indicators
	for name, led := range km.leds {
		fmt.Fprintf(b, "\tindicator \"%s\" {\n", name)
		if led.mods != 0 {
			fmt.Fprintf(b, "\t\tmodifiers= %s;\n", km.modMaskToString(led.mods))
		}
		if led.group != 0 {
			fmt.Fprintf(b, "\t\tgroups= %d;\n", led.group)
		}
		b.WriteString("\t};\n")
	}

	b.WriteString("};\n\n")
}

func (km *Keymap) writeSymbols(b *strings.Builder) {
	b.WriteString("xkb_symbols {\n")

	// Output group names
	for i, name := range km.groupNames {
		if name != "" {
			fmt.Fprintf(b, "\tname[%d]=\"%s\";\n", i+1, name)
		}
	}
	if len(km.groupNames) > 0 {
		b.WriteString("\n")
	}

	// Output keys sorted by keycode
	for kc := km.minKeycode; kc <= km.maxKeycode; kc++ {
		key, ok := km.keys[kc]
		if !ok {
			continue
		}

		name := km.keycodeNames[kc]
		if name == "" {
			continue
		}

		// Check if key has explicit type or just default symbols
		hasExplicitType := false
		for _, grp := range key.groups {
			if grp.keyType != nil && grp.keyType.name != "" {
				hasExplicitType = true
				break
			}
		}

		if hasExplicitType || len(key.groups) > 1 {
			// Multi-line format for complex keys
			fmt.Fprintf(b, "\tkey <%s> {\n", name)
			for gi, grp := range key.groups {
				if grp.keyType != nil && grp.keyType.name != "" {
					fmt.Fprintf(b, "\t\ttype= \"%s\",\n", grp.keyType.name)
				}
				syms := km.groupSymsToString(grp)
				fmt.Fprintf(b, "\t\tsymbols[%d]= [ %s ]\n", gi+1, syms)
			}
			b.WriteString("\t};\n")
		} else if len(key.groups) == 1 {
			// Single-line format for simple keys
			syms := km.groupSymsToString(key.groups[0])
			fmt.Fprintf(b, "\tkey <%s> { [ %s ] };\n", name, syms)
		}
	}

	// Output modifier_map
	for i, modName := range km.modNames {
		if modName == "" {
			continue
		}
		modMask := ModMask(1 << i)
		var keys []string
		for kc := km.minKeycode; kc <= km.maxKeycode; kc++ {
			if key, ok := km.keys[kc]; ok {
				if key.vmodmap&modMask != 0 {
					if name := km.keycodeNames[kc]; name != "" {
						keys = append(keys, "<"+name+">")
					}
				}
			}
		}
		if len(keys) > 0 {
			fmt.Fprintf(b, "\tmodifier_map %s { %s };\n", modName, strings.Join(keys, ", "))
		}
	}

	b.WriteString("};\n")
}

func (km *Keymap) modMaskToString(mask ModMask) string {
	if mask == 0 {
		return "none"
	}

	var parts []string

	// Check real modifiers
	realModNames := []string{"Shift", "Lock", "Control", "Mod1", "Mod2", "Mod3", "Mod4", "Mod5"}
	for i, name := range realModNames {
		if mask&(1<<i) != 0 {
			// Check if we have a custom name
			if km.modNames[i] != "" {
				parts = append(parts, km.modNames[i])
			} else {
				parts = append(parts, name)
			}
		}
	}

	// Check virtual modifiers
	for vmodName, vmodMask := range km.virtualMods {
		if mask&vmodMask != 0 {
			// Check if already covered by real mod
			alreadyCovered := false
			for i := 0; i < 8; i++ {
				if vmodMask&(1<<i) != 0 && mask&(1<<i) != 0 {
					alreadyCovered = true
					break
				}
			}
			if !alreadyCovered {
				parts = append(parts, vmodName)
			}
		}
	}

	if len(parts) == 0 {
		return "none"
	}

	return strings.Join(parts, "+")
}

func (km *Keymap) groupSymsToString(grp KeyGroup) string {
	var syms []string
	for _, lvl := range grp.levels {
		if len(lvl.syms) == 0 {
			syms = append(syms, "NoSymbol")
		} else if len(lvl.syms) == 1 {
			syms = append(syms, keysymToStr(lvl.syms[0]))
		} else {
			// Multiple syms at one level (rare)
			var multiSyms []string
			for _, s := range lvl.syms {
				multiSyms = append(multiSyms, keysymToStr(s))
			}
			syms = append(syms, "{ "+strings.Join(multiSyms, ", ")+" }")
		}
	}
	return strings.Join(syms, ", ")
}

// keysymToStr returns the string representation of a keysym for serialization.
// Returns the name if known, otherwise returns hex format.
func keysymToStr(ks Keysym) string {
	if ks == KeyNoSymbol {
		return "NoSymbol"
	}
	if name := KeysymGetName(ks); name != "" {
		return name
	}
	// Unknown keysym - output as hex
	return fmt.Sprintf("0x%x", ks)
}
