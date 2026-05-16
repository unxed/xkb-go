package xkb

// State tracks the active keyboard state for key translation.
//
// State is used to translate keycodes to keysyms based on the current
// modifier and group state. Create a State with [Keymap.NewState].
//
// State is NOT safe for concurrent use. Each keyboard device
// should have its own State instance.
type State struct {
	keymap *Keymap

	// Modifier state (three components combined for effective state)
	baseMods    ModMask // Currently pressed modifiers
	latchedMods ModMask // One-shot modifiers (sticky keys)
	lockedMods  ModMask // Toggled modifiers (Caps Lock, Num Lock)

	// Group state (three components combined for effective group)
	baseGroup    Group
	latchedGroup Group
	lockedGroup  Group
}

// Keymap returns the [Keymap] this state is for.
func (s *State) Keymap() *Keymap {
	return s.keymap
}
func (s *State) BaseMods() ModMask    { return s.baseMods }
func (s *State) LatchedMods() ModMask { return s.latchedMods }
func (s *State) LockedMods() ModMask  { return s.lockedMods }
func (s *State) BaseGroup() Group     { return s.baseGroup }
func (s *State) LatchedGroup() Group  { return s.latchedGroup }
func (s *State) LockedGroup() Group   { return s.lockedGroup }

// UpdateMask updates the keyboard state from modifier/group masks.
//
// This is called in response to wl_keyboard.modifiers events from
// a Wayland compositor. The masks correspond to the event parameters.
//
// Returns a bitmask of [StateComponent] flags indicating what changed.
func (s *State) UpdateMask(
	baseMods, latchedMods, lockedMods ModMask,
	baseGroup, latchedGroup, lockedGroup Group,
) StateComponent {
	var changed StateComponent

	if s.baseMods != baseMods {
		s.baseMods = baseMods
		changed |= StateModDepressed
	}
	if s.latchedMods != latchedMods {
		s.latchedMods = latchedMods
		changed |= StateModLatched
	}
	if s.lockedMods != lockedMods {
		s.lockedMods = lockedMods
		changed |= StateModLocked
	}
	if s.baseGroup != baseGroup {
		s.baseGroup = baseGroup
		changed |= StateGroupDepressed
	}
	if s.latchedGroup != latchedGroup {
		s.latchedGroup = latchedGroup
		changed |= StateGroupLatched
	}
	if s.lockedGroup != lockedGroup {
		s.lockedGroup = lockedGroup
		changed |= StateGroupLocked
	}

	if changed&StateMods != 0 {
		changed |= StateModEffective
	}
	if changed&StateGroups != 0 {
		changed |= StateGroupEffective
	}

	return changed
}

// UpdateKey updates state based on a key press/release.
//
// This is typically used with evdev input where you track key state manually.
// Wayland clients usually use [State.UpdateMask] instead, as the compositor
// provides modifier state directly.
//
// Returns a bitmask of [StateComponent] flags indicating what changed.
func (s *State) UpdateKey(keycode Keycode, direction KeyDirection) StateComponent {
	// TODO: Implement key-based state update
	// This requires looking up the key's actions and applying them
	return 0
}

// effectiveMods returns the combined modifier state.
func (s *State) effectiveMods() ModMask {
	return s.baseMods | s.latchedMods | s.lockedMods
}

// effectiveGroup returns the combined group, wrapped to valid range.
func (s *State) effectiveGroup() Group {
	total := int(s.baseGroup) + int(s.latchedGroup) + int(s.lockedGroup)
	if s.keymap.numGroups == 0 {
		return 0
	}
	return Group(total % s.keymap.numGroups)
}

// KeyGetSyms returns all keysyms for a key at the current state.
//
// Most keys return a single keysym, but some may return multiple.
// See also [State.KeyGetOneSym] for the common single-keysym case.
func (s *State) KeyGetSyms(keycode Keycode) []Keysym {
	key, ok := s.keymap.keys[keycode]
	if !ok || len(key.groups) == 0 {
		return nil
	}

	// Get effective group, wrapped to available groups
	group := s.effectiveGroup()
	if int(group) >= len(key.groups) {
		group = Group(int(group) % len(key.groups))
	}

	keyGroup := key.groups[group]
	if keyGroup.keyType == nil || len(keyGroup.levels) == 0 {
		return nil
	}

	// Get level from key type based on effective modifiers
	level := s.getLevel(keyGroup.keyType)
	if int(level) >= len(keyGroup.levels) {
		level = 0
	}

	return keyGroup.levels[level].syms
}

// KeyGetOneSym returns a single keysym for a key at the current state.
//
// If the key produces multiple keysyms, returns [KeyNoSymbol].
// If the key produces exactly one keysym, returns it.
// This is the most commonly used method for key translation.
func (s *State) KeyGetOneSym(keycode Keycode) Keysym {
	syms := s.KeyGetSyms(keycode)
	if len(syms) == 1 {
		return syms[0]
	}
	return KeyNoSymbol
}

// KeyGetUTF32 returns the Unicode codepoint for a key at the current state.
//
// Returns 0 if the key doesn't produce a character (e.g., modifier keys).
// See also [State.KeyGetUTF8] for the UTF-8 encoded string.
func (s *State) KeyGetUTF32(keycode Keycode) rune {
	sym := s.KeyGetOneSym(keycode)
	if sym == KeyNoSymbol {
		return 0
	}
	return KeysymToUTF32(sym)
}

// KeyGetUTF8 returns the UTF-8 string for a key at the current state.
//
// Returns empty string if the key doesn't produce a character.
// See also [State.KeyGetUTF32] for the raw codepoint.
func (s *State) KeyGetUTF8(keycode Keycode) string {
	r := s.KeyGetUTF32(keycode)
	if r == 0 {
		return ""
	}
	return string(r)
}

// getLevel determines the shift level based on modifiers and key type.
func (s *State) getLevel(kt *KeyType) Level {
	if kt == nil {
		return 0
	}

	// Mask modifiers to only those the key type cares about
	mods := s.effectiveMods() & kt.mods

	// Look up level in key type entries
	for _, entry := range kt.entries {
		if entry.mods == mods {
			return entry.level
		}
	}

	// Default to level 0 if no match
	return 0
}

// ModNameIsActive checks if a modifier is active.
//
// The typ parameter specifies which [StateComponent] to check
// (e.g., [StateModEffective] for combined state).
func (s *State) ModNameIsActive(name string, typ StateComponent) bool {
	idx := s.keymap.ModGetIndex(name)
	if idx < 0 {
		return false
	}
	return s.ModIndexIsActive(ModIndex(idx), typ)
}

// ModIndexIsActive checks if a modifier at the given [ModIndex] is active.
//
// See also [State.ModNameIsActive] for name-based lookup.
func (s *State) ModIndexIsActive(idx ModIndex, typ StateComponent) bool {
	mask := ModMask(1 << idx)
	var active ModMask

	if typ&StateModDepressed != 0 {
		active |= s.baseMods
	}
	if typ&StateModLatched != 0 {
		active |= s.latchedMods
	}
	if typ&StateModLocked != 0 {
		active |= s.lockedMods
	}
	if typ&StateModEffective != 0 {
		active |= s.effectiveMods()
	}

	return active&mask != 0
}

// SerializeMods returns the [ModMask] for the specified [StateComponent].
//
// Pass [StateModEffective] to get the combined modifier state.
func (s *State) SerializeMods(typ StateComponent) ModMask {
	var mods ModMask

	if typ&StateModDepressed != 0 {
		mods |= s.baseMods
	}
	if typ&StateModLatched != 0 {
		mods |= s.latchedMods
	}
	if typ&StateModLocked != 0 {
		mods |= s.lockedMods
	}
	if typ&StateModEffective != 0 {
		mods |= s.effectiveMods()
	}

	return mods
}

// SerializeGroup returns the [Group] for the specified [StateComponent].
//
// Pass [StateGroupEffective] to get the combined group state.
func (s *State) SerializeGroup(typ StateComponent) Group {
	var group int

	if typ&StateGroupDepressed != 0 {
		group += int(s.baseGroup)
	}
	if typ&StateGroupLatched != 0 {
		group += int(s.latchedGroup)
	}
	if typ&StateGroupLocked != 0 {
		group += int(s.lockedGroup)
	}
	if typ&StateGroupEffective != 0 {
		return s.effectiveGroup()
	}

	if s.keymap.numGroups > 0 {
		group = group % s.keymap.numGroups
	}
	return Group(group)
}

// LEDNameIsActive checks if an [LED] indicator should be lit.
//
// Returns true if the LED's associated modifiers or group are active.
func (s *State) LEDNameIsActive(name string) bool {
	led, ok := s.keymap.leds[name]
	if !ok {
		return false
	}

	// Check if modifiers match
	if led.mods != 0 && s.effectiveMods()&led.mods != 0 {
		return true
	}

	// Check if group matches
	if led.group != 0 && s.effectiveGroup() == led.group {
		return true
	}

	return false
}
