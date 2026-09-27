package xkb

import (
	"context"
	"encoding/binary"
	"reflect"
	"testing"
)

// This file tests the XKB X11 wire-protocol decoder (x11.go) against
// synthetic reply buffers built by hand from the XKB protocol
// specification, with no X11 server, CGO, or network connection involved.
// The wire layouts encoded here were cross-checked against libxkbcommon's
// x11 backend (src/x11/keymap.c) and the x11rb Rust XKB bindings while
// x11.go was written; see the comment at the top of x11.go for the exact
// references.

// wireBuilder is a tiny byte-buffer builder used to hand-encode synthetic
// XKB wire-protocol replies for tests.
type wireBuilder struct {
	buf []byte
}

func (w *wireBuilder) u8(v uint8) { w.buf = append(w.buf, v) }

func (w *wireBuilder) u16(v uint16) {
	var b [2]byte
	binary.LittleEndian.PutUint16(b[:], v)
	w.buf = append(w.buf, b[:]...)
}

func (w *wireBuilder) u32(v uint32) {
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], v)
	w.buf = append(w.buf, b[:]...)
}

func (w *wireBuilder) bytes(bs ...byte) { w.buf = append(w.buf, bs...) }

func (w *wireBuilder) name4(s string) {
	var b [4]byte
	copy(b[:], s)
	w.buf = append(w.buf, b[:]...)
}

func (w *wireBuilder) padTo4() {
	for len(w.buf)%4 != 0 {
		w.buf = append(w.buf, 0)
	}
}

// buildTestGetMapReply encodes a synthetic XKB GetMap reply for a tiny
// 3-key keyboard (keycodes 8-10):
//
//   - keycode 8 ("AC01"): 1 group, type ONE_LEVEL, symbol 'a'.
//   - keycode 9 ("LFSH"): 1 group, type ONE_LEVEL, symbol Shift_L; also
//     contributes the real Shift modifier via the ModifierMap section.
//   - keycode 10 ("AC02"): 1 group, type TWO_LEVEL (Shift-sensitive),
//     symbols 'b' (level 0) and 'B' (level 1).
//
// One virtual modifier ("Alt", atom resolved separately) is defined,
// mapping to the real Mod1 modifier.
func buildTestGetMapReply() []byte {
	w := &wireBuilder{}

	w.u8(1)  // response type
	w.u8(0)  // deviceID
	w.u16(0) // sequence
	w.u32(0) // length (not read by the parser)

	w.u8(8)                                                                       // minKeyCode
	w.u8(10)                                                                      // maxKeyCode
	w.u16(x11MapPartKeyTypes | x11MapPartKeySyms | x11MapPartModifierMap | x11MapPartVirtualMods) // present
	w.u8(0)  // firstType
	w.u8(2)  // nTypes
	w.u8(2)  // totalTypes
	w.u8(8)  // firstKeySym
	w.u16(4) // totalSyms
	w.u8(3)  // nKeySyms
	w.u8(0)  // firstKeyAction
	w.u16(0) // totalActions
	w.u8(0)  // nKeyActions
	w.u8(0)  // firstKeyBehavior
	w.u8(0)  // nKeyBehaviors
	w.u8(0)  // totalKeyBehaviors
	w.u8(0)  // firstKeyExplicit
	w.u8(0)  // nKeyExplicit
	w.u8(0)  // totalKeyExplicit
	w.u8(9)  // firstModMapKey
	w.u8(1)  // nModMapKeys
	w.u8(1)  // totalModMapKeys
	w.u8(0)  // firstVModMapKey
	w.u8(0)  // nVModMapKeys
	w.u8(0)  // totalVModMapKeys
	w.u8(0)  // pad
	w.u16(1) // virtualMods: 1 defined slot (bit 0, "Alt")

	// types_rtrn[0]: ONE_LEVEL
	w.u8(0)  // mods_mask
	w.u8(0)  // mods_mods
	w.u16(0) // mods_vmods
	w.u8(1)  // numLevels
	w.u8(0)  // nMapEntries
	w.u8(0)  // hasPreserve
	w.u8(0)  // pad

	// types_rtrn[1]: TWO_LEVEL, Shift -> level 1
	w.u8(0)         // mods_mask
	w.u8(uint8(ModShift)) // mods_mods
	w.u16(0)        // mods_vmods
	w.u8(2)         // numLevels
	w.u8(1)         // nMapEntries
	w.u8(0)         // hasPreserve
	w.u8(0)         // pad
	w.u8(1)         // KTMapEntry.active
	w.u8(uint8(ModShift)) // KTMapEntry.mods_mask
	w.u8(1)         // KTMapEntry.level
	w.u8(uint8(ModShift)) // KTMapEntry.mods_mods
	w.u16(0)        // KTMapEntry.mods_vmods
	w.u16(0)        // KTMapEntry pad

	// syms_rtrn[0]: keycode 8, 'a'
	w.bytes(0, 0, 0, 0) // kt_index
	w.u8(1)             // groupInfo: 1 group
	w.u8(1)             // width
	w.u16(1)            // nSyms
	w.u32(0x0061)       // 'a'

	// syms_rtrn[1]: keycode 9, Shift_L
	w.bytes(0, 0, 0, 0)
	w.u8(1)
	w.u8(1)
	w.u16(1)
	w.u32(0xffe1)

	// syms_rtrn[2]: keycode 10, type index 1 (TWO_LEVEL), 'b'/'B'
	w.bytes(1, 0, 0, 0)
	w.u8(1)
	w.u8(2)
	w.u16(2)
	w.u32(0x0062)
	w.u32(0x0042)

	// vmods_rtrn: vmod 0 ("Alt") -> real Mod1
	w.u8(uint8(ModMod1))
	w.padTo4()

	// modmap_rtrn: keycode 9 contributes real Shift
	w.u8(9)
	w.u8(uint8(ModShift))
	w.padTo4()

	return w.buf
}

// buildTestGetNamesReply encodes a synthetic XKB GetNames reply matching
// buildTestGetMapReply: the same keycode range, one key type per entry in
// the map reply, one virtual modifier name, one group name, and one key
// alias ("ALIA" for keycode 10, named "AC02").
func buildTestGetNamesReply() []byte {
	w := &wireBuilder{}

	w.u8(1)  // response type
	w.u8(0)  // deviceID
	w.u16(0) // sequence
	w.u32(0) // length (not read by the parser)

	w.u32(x11NameDetailKeyTypeNames | x11NameDetailKeyNames | x11NameDetailKeyAliases | x11NameDetailVirtualModNames | x11NameDetailGroupNames) // which
	w.u8(8)    // minKeyCode
	w.u8(10)   // maxKeyCode
	w.u8(2)    // nTypes
	w.u8(0x01) // groupNames: Group1 defined
	w.u16(1)   // virtualMods: 1 named vmod (bit 0, "Alt")
	w.u8(8)    // firstKey
	w.u8(3)    // nKeys
	w.u32(0)   // indicators
	w.u8(0)    // nRadioGroups
	w.u8(1)    // nKeyAliases
	w.u16(0)   // nKTLevels
	w.bytes(0, 0, 0, 0) // pad

	// Value list, in wire order.
	w.u32(100) // typeNames[0] atom -> "ONE_LEVEL"
	w.u32(101) // typeNames[1] atom -> "TWO_LEVEL"
	w.u32(200) // virtualModNames[0] atom -> "Alt"
	w.u32(300) // groups[0] atom -> "English (US)"
	w.name4("AC01")
	w.name4("LFSH")
	w.name4("AC02")
	w.name4("AC02") // alias.real
	w.name4("ALIA") // alias.alias

	return w.buf
}

// buildTestGetControlsReply encodes a synthetic XKB GetControls reply: one
// group, keycodes 8 and 10 auto-repeat, keycode 9 (Shift) does not.
func buildTestGetControlsReply() []byte {
	w := &wireBuilder{}

	w.u8(1)  // response type
	w.u8(0)  // deviceID
	w.u16(0) // sequence
	w.u32(0) // length (not read by the parser)

	w.u8(0)  // mouseKeysDfltBtn
	w.u8(1)  // numGroups
	w.u8(0)  // groupsWrap
	w.u8(0)  // internalModsMask
	w.u8(0)  // ignoreLockModsMask
	w.u8(0)  // internalModsRealMods
	w.u8(0)  // ignoreLockModsRealMods
	w.u8(0)  // pad
	w.u16(0) // internalModsVmods
	w.u16(0) // ignoreLockModsVmods
	w.u16(0) // repeatDelay
	w.u16(0) // repeatInterval
	w.u16(0) // slowKeysDelay
	w.u16(0) // debounceDelay
	w.u16(0) // mouseKeysDelay
	w.u16(0) // mouseKeysInterval
	w.u16(0) // mouseKeysTimeToMax
	w.u16(0) // mouseKeysMaxSpeed
	w.u16(0) // mouseKeysCurve
	w.u16(0) // accessXOption
	w.u16(0) // accessXTimeout
	w.u16(0) // accessXTimeoutOptionsMask
	w.u16(0) // accessXTimeoutOptionsValues
	w.u16(0) // pad
	w.u32(0) // accessXTimeoutMask
	w.u32(0) // accessXTimeoutValues
	w.u32(0) // enabledControls

	perKeyRepeat := make([]byte, 32)
	perKeyRepeat[8/8] |= 1 << (8 % 8)
	perKeyRepeat[10/8] |= 1 << (10 % 8)
	w.bytes(perKeyRepeat...)

	if len(w.buf) != 92 {
		panic("buildTestGetControlsReply: internal size mismatch")
	}
	return w.buf
}

func TestParseX11GetMapReply(t *testing.T) {
	got, err := ParseX11GetMapReply(buildTestGetMapReply())
	if err != nil {
		t.Fatalf("ParseX11GetMapReply: %v", err)
	}

	if got.MinKeyCode != 8 || got.MaxKeyCode != 10 {
		t.Fatalf("keycode range = [%d, %d], want [8, 10]", got.MinKeyCode, got.MaxKeyCode)
	}
	if len(got.Types) != 2 {
		t.Fatalf("len(Types) = %d, want 2", len(got.Types))
	}
	if got.Types[0].NumLevels != 1 {
		t.Errorf("Types[0].NumLevels = %d, want 1", got.Types[0].NumLevels)
	}
	if got.Types[1].NumLevels != 2 || got.Types[1].Mods != ModShift {
		t.Errorf("Types[1] = %+v, want NumLevels=2 Mods=ModShift", got.Types[1])
	}
	if len(got.Types[1].Entries) != 1 || got.Types[1].Entries[0].Mods != ModShift || got.Types[1].Entries[0].Level != 1 {
		t.Errorf("Types[1].Entries = %+v, want a single Shift->level 1 entry", got.Types[1].Entries)
	}

	if len(got.KeySyms) != 3 {
		t.Fatalf("len(KeySyms) = %d, want 3", len(got.KeySyms))
	}
	if !reflect.DeepEqual(got.KeySyms[0].Syms, []Keysym{0x0061}) {
		t.Errorf("KeySyms[0].Syms = %v, want ['a']", got.KeySyms[0].Syms)
	}
	if !reflect.DeepEqual(got.KeySyms[2].Syms, []Keysym{0x0062, 0x0042}) {
		t.Errorf("KeySyms[2].Syms = %v, want ['b', 'B']", got.KeySyms[2].Syms)
	}
	if got.KeySyms[2].TypeIndex[0] != 1 {
		t.Errorf("KeySyms[2].TypeIndex[0] = %d, want 1", got.KeySyms[2].TypeIndex[0])
	}

	if len(got.ModMap) != 1 || got.ModMap[0].Keycode != 9 || got.ModMap[0].Mods != ModShift {
		t.Errorf("ModMap = %+v, want a single keycode-9 Shift entry", got.ModMap)
	}
}

func TestParseX11GetMapReplyTruncated(t *testing.T) {
	full := buildTestGetMapReply()
	for _, n := range []int{0, 10, 38, len(full) - 1} {
		if _, err := ParseX11GetMapReply(full[:n]); err == nil {
			t.Errorf("ParseX11GetMapReply(%d bytes) = nil error, want an error", n)
		}
	}
}

func TestParseX11GetNamesReply(t *testing.T) {
	got, err := ParseX11GetNamesReply(buildTestGetNamesReply())
	if err != nil {
		t.Fatalf("ParseX11GetNamesReply: %v", err)
	}

	wantKeyNames := []string{"AC01", "LFSH", "AC02"}
	if !reflect.DeepEqual(got.KeyNames, wantKeyNames) {
		t.Errorf("KeyNames = %v, want %v", got.KeyNames, wantKeyNames)
	}
	if !reflect.DeepEqual(got.TypeNameAtoms, []uint32{100, 101}) {
		t.Errorf("TypeNameAtoms = %v, want [100, 101]", got.TypeNameAtoms)
	}
	if !reflect.DeepEqual(got.VirtualModNameAtoms, []uint32{200}) {
		t.Errorf("VirtualModNameAtoms = %v, want [200]", got.VirtualModNameAtoms)
	}
	if !reflect.DeepEqual(got.GroupNameAtoms, []uint32{300}) {
		t.Errorf("GroupNameAtoms = %v, want [300]", got.GroupNameAtoms)
	}
	wantAliases := []X11KeyAlias{{Real: "AC02", Alias: "ALIA"}}
	if !reflect.DeepEqual(got.Aliases, wantAliases) {
		t.Errorf("Aliases = %+v, want %+v", got.Aliases, wantAliases)
	}
}

func TestParseX11GetControlsReply(t *testing.T) {
	got, err := ParseX11GetControlsReply(buildTestGetControlsReply())
	if err != nil {
		t.Fatalf("ParseX11GetControlsReply: %v", err)
	}
	if got.NumGroups != 1 {
		t.Errorf("NumGroups = %d, want 1", got.NumGroups)
	}
	repeats := func(kc int) bool {
		return got.PerKeyRepeat[kc/8]&(1<<uint(kc%8)) != 0
	}
	if !repeats(8) || repeats(9) || !repeats(10) {
		t.Errorf("repeats(8,9,10) = (%v,%v,%v), want (true,false,true)", repeats(8), repeats(9), repeats(10))
	}
}

func TestNewKeymapFromX11Replies(t *testing.T) {
	mapReply, err := ParseX11GetMapReply(buildTestGetMapReply())
	if err != nil {
		t.Fatalf("ParseX11GetMapReply: %v", err)
	}
	namesReply, err := ParseX11GetNamesReply(buildTestGetNamesReply())
	if err != nil {
		t.Fatalf("ParseX11GetNamesReply: %v", err)
	}
	controlsReply, err := ParseX11GetControlsReply(buildTestGetControlsReply())
	if err != nil {
		t.Fatalf("ParseX11GetControlsReply: %v", err)
	}

	atomNames := map[uint32]string{
		100: "ONE_LEVEL",
		101: "TWO_LEVEL",
		200: "Alt",
		300: "English (US)",
	}

	ctx := NewContext(context.Background(), ContextNoFlags)
	km, err := NewKeymapFromX11Replies(ctx, mapReply, namesReply, controlsReply, atomNames)
	if err != nil {
		t.Fatalf("NewKeymapFromX11Replies: %v", err)
	}

	if km.Context() != ctx {
		t.Error("Context() does not return the Context passed to NewKeymapFromX11Replies")
	}
	if km.MinKeycode() != 8 || km.MaxKeycode() != 10 {
		t.Fatalf("keycode range = [%d, %d], want [8, 10]", km.MinKeycode(), km.MaxKeycode())
	}
	if km.NumGroups() != 1 {
		t.Errorf("NumGroups() = %d, want 1", km.NumGroups())
	}
	if got := km.GroupName(0); got != "English (US)" {
		t.Errorf("GroupName(0) = %q, want \"English (US)\"", got)
	}
	if km.NumTypes() != 2 {
		t.Errorf("NumTypes() = %d, want 2", km.NumTypes())
	}

	for kc, want := range map[Keycode]string{8: "AC01", 9: "LFSH", 10: "AC02"} {
		if got := km.KeyGetName(kc); got != want {
			t.Errorf("KeyGetName(%d) = %q, want %q", kc, got, want)
		}
	}
	if got := km.KeyByName("AC01"); got != 8 {
		t.Errorf(`KeyByName("AC01") = %d, want 8`, got)
	}
	if got := km.KeyByName("ALIA"); got != 10 {
		t.Errorf(`KeyByName("ALIA") = %d, want 10 (via alias of AC02)`, got)
	}

	for kc, want := range map[Keycode]bool{8: true, 9: false, 10: true} {
		if got := km.KeyRepeats(kc); got != want {
			t.Errorf("KeyRepeats(%d) = %v, want %v", kc, got, want)
		}
	}

	if idx := km.ModGetIndex("Alt"); ModMask(1<<uint(idx)) != ModMod1 {
		t.Errorf("ModGetIndex(\"Alt\") = %d, want the index of ModMod1", idx)
	}

	state := km.NewState()
	if sym := state.KeyGetOneSym(8); sym != 0x0061 {
		t.Errorf("KeyGetOneSym(8) = %#x, want 'a' (0x61)", sym)
	}
	if sym := state.KeyGetOneSym(10); sym != 0x0062 {
		t.Errorf("KeyGetOneSym(10) with no modifiers = %#x, want 'b' (0x62)", sym)
	}
	state.UpdateMask(ModShift, 0, 0, 0, 0, 0)
	if sym := state.KeyGetOneSym(10); sym != 0x0042 {
		t.Errorf("KeyGetOneSym(10) with Shift = %#x, want 'B' (0x42)", sym)
	}
}

func TestNewKeymapFromX11RepliesRejectsMismatchedRanges(t *testing.T) {
	mapReply, err := ParseX11GetMapReply(buildTestGetMapReply())
	if err != nil {
		t.Fatalf("ParseX11GetMapReply: %v", err)
	}
	namesReply, err := ParseX11GetNamesReply(buildTestGetNamesReply())
	if err != nil {
		t.Fatalf("ParseX11GetNamesReply: %v", err)
	}
	controlsReply, err := ParseX11GetControlsReply(buildTestGetControlsReply())
	if err != nil {
		t.Fatalf("ParseX11GetControlsReply: %v", err)
	}

	namesReply.MaxKeyCode = mapReply.MaxKeyCode + 1

	ctx := NewContext(context.Background(), ContextNoFlags)
	if _, err := NewKeymapFromX11Replies(ctx, mapReply, namesReply, controlsReply, nil); err == nil {
		t.Fatal("NewKeymapFromX11Replies with mismatched keycode ranges = nil error, want an error")
	}
}
