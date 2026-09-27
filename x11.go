package xkb

import (
	"encoding/binary"
	"fmt"
	"math/bits"
)

// This file implements a pure Go, network-transport-agnostic decoder for the
// X Keyboard Extension (XKB) X11 wire protocol replies to GetMap, GetNames
// and GetControls, and a builder that turns the decoded data into a
// [Keymap] -- the same structure produced by [Context.NewKeymapFromString]
// and [Context.NewKeymapFromNames].
//
// It deliberately has no dependency on any X11 transport library: the
// Parse* functions operate on raw reply byte buffers (as returned by any
// X11 client library, or synthesized in tests), and [NewKeymapFromX11Replies]
// only needs the decoded data plus a resolved map of X11 ATOM IDs to names
// (since ATOM name resolution itself requires a GetAtomName round trip on
// a live connection). This keeps the xkb-go module free of any X11
// transport dependency; see the companion package
// github.com/thegrumpylion/xkb-go/x11 for a ready-to-use implementation on top of
// github.com/jezek/xgb that sends the requests, resolves the atoms and
// calls into this file.
//
// Wire layouts follow the XKB X11 protocol extension specification
// (https://www.x.org/releases/current/doc/kbproto/xkbproto.html) and the
// xkb.xml protocol description shipped with xcb-proto. The libxkbcommon
// x11 backend (src/x11/keymap.c, xkb_x11_keymap_new_from_device) implements
// the same protocol in C and was used as a cross-check while writing this
// file.
//
// Scope: only the subset of GetMap/GetNames/GetControls needed to populate
// [Keymap] is requested and parsed: key types, per-key symbols, the real
// modifier map, and the virtual-modifier-to-real-modifier table (GetMap);
// key/type/group/virtual-modifier names and key aliases (GetNames); and
// per-key auto-repeat plus the group count (GetControls). Indicator (LED)
// maps, key actions/behaviors, explicit-component flags, the per-key
// virtual modifier map, and the compat map (symbol interprets) are not
// requested: the X server has already resolved all of that into the
// effective key types and symbols returned by GetMap, so they are not
// needed to translate keys with the resulting Keymap. A later change can
// add LED support (GetIndicatorMap) if xkb-go grows a use for it.

// x11Pad4 rounds n up to the next multiple of 4, matching the alignment
// padding the X11 wire protocol inserts after some variable-length list
// fields.
func x11Pad4(n int) int {
	return (n + 3) &^ 3
}

// --- GetMap -----------------------------------------------------------

// X11KTMapEntry mirrors one entry of a wire KeyType's map list.
type X11KTMapEntry struct {
	// Mods is the combined (real + virtual, resolved to real bits) modifier
	// mask for this entry, as XKB modifier names resolve in xkb-go.
	Mods ModMask
	// Level is the 0-based shift level this entry selects.
	Level Level
	// Preserve is the combined modifier mask preserved by this entry, if
	// the wire KeyType included preserve data.
	Preserve ModMask
}

// X11KeyType mirrors a wire KeyType structure.
type X11KeyType struct {
	// Mods is the combined (real + virtual, resolved to real bits) set of
	// modifiers this type considers.
	Mods      ModMask
	NumLevels int
	Entries   []X11KTMapEntry
}

// X11KeyModMap is one entry of the wire ModifierMap ("real" modifier map)
// list: it says which real modifiers a key contributes when held, exactly
// like an xkb_symbols `modifier_map` statement.
type X11KeyModMap struct {
	Keycode Keycode
	Mods    ModMask
}

// X11MapReply is the parsed result of an XKB GetMap reply.
type X11MapReply struct {
	MinKeyCode Keycode
	MaxKeyCode Keycode

	Types []X11KeyType

	// KeySyms holds one entry per keycode in [MinKeyCode, MaxKeyCode],
	// in that order.
	KeySyms []X11KeySymMap

	ModMap []X11KeyModMap

	// vmodRealMods carries the raw per-slot virtual-modifier data needed by
	// NewKeymapFromX11Replies to build the Keymap's virtualMods table once
	// virtual modifier names are known (from X11NamesReply). It is not
	// part of the public API of this struct.
	vmodRealMods [16]ModMask
}

// X11KeySymMap mirrors a wire KeySymMap structure: the per-key type
// indices, group/level layout, and flattened symbol list for one keycode.
type X11KeySymMap struct {
	// TypeIndex holds, for each group (0..NumGroups-1), the index into
	// [X11MapReply.Types] of that group's key type.
	TypeIndex [4]uint8
	// NumGroups is the number of groups (layouts) this key defines (0-4).
	NumGroups int
	// Width is the number of levels per group.
	Width int
	// Syms is the flattened [group][level] symbol table, Width*NumGroups
	// entries long, in group-major order.
	Syms []Keysym
}

// GetMap MapPart bits actually requested/parsed. See the MapPart enum in
// xkb.xml (the xcb-proto XKB protocol description).
const (
	x11MapPartKeyTypes    = 1 << 0
	x11MapPartKeySyms     = 1 << 1
	x11MapPartModifierMap = 1 << 2
	x11MapPartVirtualMods = 1 << 6
)

// X11GetMapWanted is the GetMap request `full`/`partial` MapPart mask
// xkb-go needs. Pass it as the `full` field of a GetMap request, with
// `partial` and all first*/n* fields zero, to request the sections
// [ParseX11GetMapReply] understands for the complete keycode range.
const X11GetMapWanted = x11MapPartKeyTypes | x11MapPartKeySyms | x11MapPartModifierMap | x11MapPartVirtualMods

// ParseX11GetMapReply decodes a raw XKB GetMap reply (as returned by the X
// server for a request built with `full` = [X11GetMapWanted]) into an
// [X11MapReply].
//
// buf must be the complete reply, starting at its response-type byte (byte
// 0 of the X11 reply), as delivered by the transport (e.g. what
// jezek/xgb's Cookie.Reply returns).
func ParseX11GetMapReply(buf []byte) (*X11MapReply, error) {
	// Fixed part: 8-byte generic X11 reply header + 30 bytes of GetMap
	// reply-specific fields ending right before the variable "map" data.
	const fixedHeaderLen = 38
	if len(buf) < fixedHeaderLen {
		return nil, fmt.Errorf("xkb: GetMap reply too short (%d bytes)", len(buf))
	}
	if buf[0] != 1 {
		return nil, fmt.Errorf("xkb: GetMap reply has unexpected response type %d", buf[0])
	}

	minKeyCode := Keycode(buf[8])
	maxKeyCode := Keycode(buf[9])
	present := binary.LittleEndian.Uint16(buf[10:])
	nTypes := int(buf[13])
	nKeySyms := int(buf[18])
	totalModMapKeys := int(buf[31])
	virtualMods := binary.LittleEndian.Uint16(buf[36:])

	if minKeyCode > maxKeyCode {
		return nil, fmt.Errorf("xkb: GetMap reply has minKeyCode %d > maxKeyCode %d", minKeyCode, maxKeyCode)
	}
	const required = x11MapPartKeyTypes | x11MapPartKeySyms
	if present&required != required {
		return nil, fmt.Errorf("xkb: GetMap reply is missing required sections (present=0x%x)", present)
	}

	reply := &X11MapReply{
		MinKeyCode: minKeyCode,
		MaxKeyCode: maxKeyCode,
	}

	// vmodRealMods[i] is the real-modifier byte the server reports for
	// virtual modifier slot i, if the VirtualMods section is present.
	var vmodRealMods [16]ModMask

	b := fixedHeaderLen

	// KeyTypes.
	rawTypes := make([]rawX11KeyType, nTypes)
	for i := 0; i < nTypes; i++ {
		wt, next, err := parseX11KeyTypeWire(buf, b)
		if err != nil {
			return nil, err
		}
		rawTypes[i] = wt
		b = next
	}

	// KeySyms.
	rawSyms := make([]X11KeySymMap, nKeySyms)
	for i := 0; i < nKeySyms; i++ {
		sm, next, err := parseX11KeySymMapWire(buf, b)
		if err != nil {
			return nil, err
		}
		rawSyms[i] = sm
		b = next
	}
	if nKeySyms != int(maxKeyCode)-int(minKeyCode)+1 {
		return nil, fmt.Errorf("xkb: GetMap reply KeySyms section does not cover the full keycode range (got %d entries, want %d)",
			nKeySyms, int(maxKeyCode)-int(minKeyCode)+1)
	}
	reply.KeySyms = rawSyms

	// VirtualMods: real-modifier byte per defined virtual modifier index.
	if present&x11MapPartVirtualMods != 0 {
		n := bits.OnesCount16(virtualMods)
		if b+n > len(buf) {
			return nil, fmt.Errorf("xkb: GetMap reply truncated in VirtualMods section")
		}
		idx := 0
		for i := 0; i < 16; i++ {
			if virtualMods&(1<<uint(i)) != 0 {
				vmodRealMods[i] = ModMask(buf[b+idx])
				idx++
			}
		}
		b += n
		b = x11Pad4(b)
	}

	// ModifierMap (the "real" per-key modifier map).
	if present&x11MapPartModifierMap != 0 {
		if b+totalModMapKeys*2 > len(buf) {
			return nil, fmt.Errorf("xkb: GetMap reply truncated in ModifierMap section")
		}
		modMap := make([]X11KeyModMap, totalModMapKeys)
		for i := 0; i < totalModMapKeys; i++ {
			modMap[i] = X11KeyModMap{
				Keycode: Keycode(buf[b]),
				Mods:    ModMask(buf[b+1]),
			}
			b += 2
		}
		reply.ModMap = modMap
		b = x11Pad4(b)
	}

	if b > len(buf) {
		return nil, fmt.Errorf("xkb: GetMap reply truncated (expected at least %d bytes, got %d)", b, len(buf))
	}

	// Resolve each type's real+virtual modifiers down to real modifier
	// bits, matching how xkb-go's text parser resolves virtual modifier
	// names (see modifierNameToMask in parser.go): a type or map entry
	// that references virtual modifiers ends up with a plain ModMask of
	// the real modifiers those virtual modifiers are bound to.
	combine := func(realMods ModMask, vmods uint16) ModMask {
		m := realMods
		for i := 0; i < 16; i++ {
			if vmods&(1<<uint(i)) != 0 {
				m |= vmodRealMods[i]
			}
		}
		return m
	}

	types := make([]X11KeyType, nTypes)
	for i, wt := range rawTypes {
		kt := X11KeyType{
			Mods:      combine(wt.modsMods, wt.modsVMods),
			NumLevels: int(wt.numLevels),
		}
		entries := make([]X11KTMapEntry, len(wt.entries))
		for j, e := range wt.entries {
			entry := X11KTMapEntry{
				Mods:  combine(e.modsMods, e.modsVMods),
				Level: Level(e.level),
			}
			if j < len(wt.preserve) {
				entry.Preserve = combine(wt.preserve[j].realMods, wt.preserve[j].vmods)
			}
			entries[j] = entry
		}
		kt.Entries = entries
		types[i] = kt
	}
	reply.Types = types
	reply.vmodRealMods = vmodRealMods

	return reply, nil
}

// rawX11KTMapEntry mirrors the wire KTMapEntry structure before modifier
// resolution (8 bytes on the wire).
type rawX11KTMapEntry struct {
	modsMods  ModMask
	modsVMods uint16
	level     uint8
}

// rawX11ModDef mirrors the wire ModDef structure (4 bytes on the wire),
// used for a KeyType's preserve list.
type rawX11ModDef struct {
	realMods ModMask
	vmods    uint16
}

// rawX11KeyType mirrors the wire KeyType structure before modifier
// resolution.
type rawX11KeyType struct {
	modsMods  ModMask
	modsVMods uint16
	numLevels uint8
	entries   []rawX11KTMapEntry
	preserve  []rawX11ModDef
}

// parseX11KeyTypeWire parses one wire KeyType starting at offset b, and
// returns it along with the offset just past it.
func parseX11KeyTypeWire(buf []byte, b int) (rawX11KeyType, int, error) {
	if b+8 > len(buf) {
		return rawX11KeyType{}, 0, fmt.Errorf("xkb: GetMap reply truncated in KeyType header")
	}
	wt := rawX11KeyType{
		modsMods:  ModMask(buf[b+1]),
		modsVMods: binary.LittleEndian.Uint16(buf[b+2:]),
		numLevels: buf[b+4],
	}
	nMapEntries := int(buf[b+5])
	hasPreserve := buf[b+6] != 0
	b += 8

	if wt.numLevels == 0 {
		return rawX11KeyType{}, 0, fmt.Errorf("xkb: GetMap reply has a KeyType with zero levels")
	}

	if b+nMapEntries*8 > len(buf) {
		return rawX11KeyType{}, 0, fmt.Errorf("xkb: GetMap reply truncated in KeyType map entries")
	}
	entries := make([]rawX11KTMapEntry, nMapEntries)
	for i := 0; i < nMapEntries; i++ {
		// Wire layout: active(1) mods_mask(1) level(1) mods_mods(1)
		// mods_vmods(2) pad(2). We don't need active or mods_mask.
		entries[i] = rawX11KTMapEntry{
			level:     buf[b+2],
			modsMods:  ModMask(buf[b+3]),
			modsVMods: binary.LittleEndian.Uint16(buf[b+4:]),
		}
		b += 8
	}
	wt.entries = entries

	if hasPreserve {
		if b+nMapEntries*4 > len(buf) {
			return rawX11KeyType{}, 0, fmt.Errorf("xkb: GetMap reply truncated in KeyType preserve list")
		}
		preserve := make([]rawX11ModDef, nMapEntries)
		for i := 0; i < nMapEntries; i++ {
			// Wire layout: mask(1) realMods(1) vmods(2).
			preserve[i] = rawX11ModDef{
				realMods: ModMask(buf[b+1]),
				vmods:    binary.LittleEndian.Uint16(buf[b+2:]),
			}
			b += 4
		}
		wt.preserve = preserve
	}

	return wt, b, nil
}

// parseX11KeySymMapWire parses one wire KeySymMap starting at offset b, and
// returns it along with the offset just past it.
func parseX11KeySymMapWire(buf []byte, b int) (X11KeySymMap, int, error) {
	if b+8 > len(buf) {
		return X11KeySymMap{}, 0, fmt.Errorf("xkb: GetMap reply truncated in KeySymMap header")
	}
	var sm X11KeySymMap
	copy(sm.TypeIndex[:], buf[b:b+4])
	groupInfo := buf[b+4]
	sm.NumGroups = int(groupInfo & 0x0f)
	if sm.NumGroups > 4 {
		return X11KeySymMap{}, 0, fmt.Errorf("xkb: GetMap reply has a key with an invalid group count %d", sm.NumGroups)
	}
	sm.Width = int(buf[b+5])
	nSyms := int(binary.LittleEndian.Uint16(buf[b+6:]))
	b += 8

	if nSyms != sm.Width*sm.NumGroups {
		return X11KeySymMap{}, 0, fmt.Errorf("xkb: GetMap reply key symbol count %d does not match width %d * groups %d",
			nSyms, sm.Width, sm.NumGroups)
	}
	if b+nSyms*4 > len(buf) {
		return X11KeySymMap{}, 0, fmt.Errorf("xkb: GetMap reply truncated in KeySymMap symbol list")
	}
	syms := make([]Keysym, nSyms)
	for i := 0; i < nSyms; i++ {
		syms[i] = Keysym(binary.LittleEndian.Uint32(buf[b:]))
		b += 4
	}
	sm.Syms = syms

	return sm, b, nil
}

// --- GetNames -----------------------------------------------------------

// X11KeyAlias is one entry of the wire KeyAliases list: alias is another
// valid name for the key named real.
type X11KeyAlias struct {
	Real  string
	Alias string
}

// GetNames NameDetail bits actually requested/parsed. See the NameDetail
// enum in xkb.xml.
const (
	x11NameDetailKeyTypeNames    = 1 << 6
	x11NameDetailKeyNames        = 1 << 9
	x11NameDetailKeyAliases      = 1 << 10
	x11NameDetailVirtualModNames = 1 << 11
	x11NameDetailGroupNames      = 1 << 12
)

// X11GetNamesWanted is the GetNames request `which` NameDetail mask
// xkb-go needs.
const X11GetNamesWanted = x11NameDetailKeyTypeNames | x11NameDetailKeyNames | x11NameDetailKeyAliases | x11NameDetailVirtualModNames | x11NameDetailGroupNames

// X11NamesReply is the parsed result of an XKB GetNames reply. Atom IDs
// are left unresolved (X11 ATOMs are only meaningful with a live
// connection to resolve them to strings via GetAtomName); pass the
// resolved names to [NewKeymapFromX11Replies] via its atomNames map.
type X11NamesReply struct {
	MinKeyCode Keycode
	MaxKeyCode Keycode
	FirstKey   Keycode

	// GroupNamesMask has one bit set (bit 0..3) per defined group name, in
	// the same order as GroupNameAtoms.
	GroupNamesMask uint8
	// VirtualModsDefined has one bit set (0..15) per named virtual
	// modifier, in the same order as VirtualModNameAtoms.
	VirtualModsDefined uint16

	TypeNameAtoms       []uint32
	VirtualModNameAtoms []uint32
	GroupNameAtoms      []uint32

	// KeyNames holds one entry per keycode in [MinKeyCode, MaxKeyCode], in
	// that order; unnamed keycodes have an empty string.
	KeyNames []string
	Aliases  []X11KeyAlias
}

// ParseX11GetNamesReply decodes a raw XKB GetNames reply (as returned by
// the X server for a request built with `which` = [X11GetNamesWanted])
// into an [X11NamesReply].
func ParseX11GetNamesReply(buf []byte) (*X11NamesReply, error) {
	const fixedHeaderLen = 32
	if len(buf) < fixedHeaderLen {
		return nil, fmt.Errorf("xkb: GetNames reply too short (%d bytes)", len(buf))
	}
	if buf[0] != 1 {
		return nil, fmt.Errorf("xkb: GetNames reply has unexpected response type %d", buf[0])
	}

	which := binary.LittleEndian.Uint32(buf[8:])
	minKeyCode := Keycode(buf[12])
	maxKeyCode := Keycode(buf[13])
	nTypes := int(buf[14])
	groupNamesMask := buf[15]
	virtualMods := binary.LittleEndian.Uint16(buf[16:])
	firstKey := Keycode(buf[18])
	nKeys := int(buf[19])
	nKeyAliases := int(buf[25])

	const required = x11NameDetailKeyTypeNames | x11NameDetailKeyNames | x11NameDetailVirtualModNames
	if which&required != required {
		return nil, fmt.Errorf("xkb: GetNames reply is missing required sections (which=0x%x)", which)
	}
	if nKeys != int(maxKeyCode)-int(minKeyCode)+1 {
		return nil, fmt.Errorf("xkb: GetNames reply KeyNames section does not cover the full keycode range (got %d entries, want %d)",
			nKeys, int(maxKeyCode)-int(minKeyCode)+1)
	}

	reply := &X11NamesReply{
		MinKeyCode:         minKeyCode,
		MaxKeyCode:         maxKeyCode,
		FirstKey:           firstKey,
		GroupNamesMask:     groupNamesMask,
		VirtualModsDefined: virtualMods,
	}

	b := fixedHeaderLen

	readAtoms := func(n int) ([]uint32, error) {
		if b+n*4 > len(buf) {
			return nil, fmt.Errorf("xkb: GetNames reply truncated reading atoms")
		}
		atoms := make([]uint32, n)
		for i := 0; i < n; i++ {
			atoms[i] = binary.LittleEndian.Uint32(buf[b:])
			b += 4
		}
		return atoms, nil
	}

	// The order below is the wire order of the GetNames value-list switch
	// in xkb.xml, which is not simply increasing bit order.
	var err error
	if which&x11NameDetailKeyTypeNames != 0 {
		if reply.TypeNameAtoms, err = readAtoms(nTypes); err != nil {
			return nil, err
		}
	}
	if which&x11NameDetailVirtualModNames != 0 {
		if reply.VirtualModNameAtoms, err = readAtoms(bits.OnesCount16(virtualMods)); err != nil {
			return nil, err
		}
	}
	if which&x11NameDetailGroupNames != 0 {
		if reply.GroupNameAtoms, err = readAtoms(bits.OnesCount8(groupNamesMask)); err != nil {
			return nil, err
		}
	}
	if which&x11NameDetailKeyNames != 0 {
		if b+nKeys*4 > len(buf) {
			return nil, fmt.Errorf("xkb: GetNames reply truncated reading key names")
		}
		names := make([]string, nKeys)
		for i := 0; i < nKeys; i++ {
			names[i] = x11TrimName(buf[b : b+4])
			b += 4
		}
		reply.KeyNames = names
	}
	if which&x11NameDetailKeyAliases != 0 {
		if b+nKeyAliases*8 > len(buf) {
			return nil, fmt.Errorf("xkb: GetNames reply truncated reading key aliases")
		}
		aliases := make([]X11KeyAlias, nKeyAliases)
		for i := 0; i < nKeyAliases; i++ {
			aliases[i] = X11KeyAlias{
				Real:  x11TrimName(buf[b : b+4]),
				Alias: x11TrimName(buf[b+4 : b+8]),
			}
			b += 8
		}
		reply.Aliases = aliases
	}

	return reply, nil
}

// x11TrimName trims trailing NUL bytes from a fixed-size XKB wire name
// field and returns it as a string.
func x11TrimName(b []byte) string {
	n := 0
	for n < len(b) && b[n] != 0 {
		n++
	}
	return string(b[:n])
}

// --- GetControls ----------------------------------------------------------

// X11ControlsReply is the parsed result of an XKB GetControls reply,
// limited to the fields xkb-go needs.
type X11ControlsReply struct {
	NumGroups int
	// PerKeyRepeat is a 256-bit (32-byte) bitmap, one bit per keycode
	// (bit (keycode%8) of byte keycode/8), reporting whether that key
	// auto-repeats.
	PerKeyRepeat [32]byte
}

// ParseX11GetControlsReply decodes a raw XKB GetControls reply into an
// [X11ControlsReply].
func ParseX11GetControlsReply(buf []byte) (*X11ControlsReply, error) {
	const replyLen = 92
	if len(buf) < replyLen {
		return nil, fmt.Errorf("xkb: GetControls reply too short (%d bytes)", len(buf))
	}
	if buf[0] != 1 {
		return nil, fmt.Errorf("xkb: GetControls reply has unexpected response type %d", buf[0])
	}

	reply := &X11ControlsReply{
		NumGroups: int(buf[9]),
	}
	if reply.NumGroups == 0 || reply.NumGroups > 4 {
		return nil, fmt.Errorf("xkb: GetControls reply reports an invalid group count %d", reply.NumGroups)
	}
	copy(reply.PerKeyRepeat[:], buf[60:92])

	return reply, nil
}

// --- Keymap construction ---------------------------------------------------

// NewKeymapFromX11Replies builds a [Keymap] from the decoded replies of an
// XKB GetMap, GetNames and GetControls request sequence issued for the
// same device, plus a map from X11 ATOM ID to its resolved name (as
// referenced by mapReply and namesReply -- see [X11NamesReply.TypeNameAtoms],
// [X11NamesReply.VirtualModNameAtoms] and [X11NamesReply.GroupNameAtoms]).
//
// This is the pure counterpart of xkb_x11_keymap_new_from_device: it does
// not perform any I/O. See the github.com/thegrumpylion/xkb-go/x11 package for a
// ready-to-use implementation that sends the requests over a
// github.com/jezek/xgb connection, resolves the atoms, and calls this
// function.
func NewKeymapFromX11Replies(ctx *Context, mapReply *X11MapReply, namesReply *X11NamesReply, controlsReply *X11ControlsReply, atomNames map[uint32]string) (*Keymap, error) {
	const op = "NewKeymapFromX11Replies"

	if mapReply == nil || namesReply == nil || controlsReply == nil {
		return nil, &Error{Op: op, Err: fmt.Errorf("mapReply, namesReply and controlsReply must not be nil")}
	}
	if mapReply.MinKeyCode != namesReply.MinKeyCode || mapReply.MaxKeyCode != namesReply.MaxKeyCode {
		return nil, &Error{Op: op, Err: fmt.Errorf("GetMap and GetNames disagree on the keycode range (%d-%d vs %d-%d)",
			mapReply.MinKeyCode, mapReply.MaxKeyCode, namesReply.MinKeyCode, namesReply.MaxKeyCode)}
	}
	if namesReply.FirstKey != mapReply.MinKeyCode {
		return nil, &Error{Op: op, Err: fmt.Errorf("GetNames key range does not start at the minimum keycode")}
	}
	if len(mapReply.KeySyms) != int(mapReply.MaxKeyCode)-int(mapReply.MinKeyCode)+1 {
		return nil, &Error{Op: op, Err: fmt.Errorf("GetMap key symbol map does not cover the full keycode range")}
	}
	if len(namesReply.KeyNames) != len(mapReply.KeySyms) {
		return nil, &Error{Op: op, Err: fmt.Errorf("GetNames returned an unexpected number of key names")}
	}

	km := &Keymap{
		ctx:            ctx,
		keycodeNames:   make(map[Keycode]string),
		keycodesByName: make(map[string]Keycode),
		minKeycode:     mapReply.MinKeyCode,
		maxKeycode:     mapReply.MaxKeyCode,
		types:          make(map[string]*KeyType),
		keys:           make(map[Keycode]*Key),
		modNames:       [8]string{"Shift", "Lock", "Control", "Mod1", "Mod2", "Mod3", "Mod4", "Mod5"},
		virtualMods:    make(map[string]ModMask),
		leds:           make(map[string]*LED),
		groupNames:     make([]string, 4),
		numGroups:      controlsReply.NumGroups,
	}

	// Key types.
	typesList := make([]*KeyType, len(mapReply.Types))
	for i, wt := range mapReply.Types {
		name := fmt.Sprintf("TYPE_%d", i)
		if i < len(namesReply.TypeNameAtoms) {
			if n := atomNames[namesReply.TypeNameAtoms[i]]; n != "" {
				name = n
			}
		}
		entries := make([]KeyTypeEntry, len(wt.Entries))
		for j, e := range wt.Entries {
			entries[j] = KeyTypeEntry{
				mods:     e.Mods,
				level:    e.Level,
				preserve: e.Preserve,
			}
		}
		kt := &KeyType{
			name:      name,
			mods:      wt.Mods,
			numLevels: wt.NumLevels,
			entries:   entries,
		}
		typesList[i] = kt
		km.types[name] = kt
	}
	km.typesList = typesList

	// Virtual modifier names -> real modifier mask.
	vmodIdx := 0
	for i := 0; i < 16; i++ {
		if namesReply.VirtualModsDefined&(1<<uint(i)) == 0 {
			continue
		}
		name := fmt.Sprintf("VMod%d", i)
		if vmodIdx < len(namesReply.VirtualModNameAtoms) {
			if n := atomNames[namesReply.VirtualModNameAtoms[vmodIdx]]; n != "" {
				name = n
			}
		}
		vmodIdx++
		km.virtualMods[name] = mapReply.vmodRealMods[i]
	}

	// Group names.
	groupIdx := 0
	for i := 0; i < 4; i++ {
		if namesReply.GroupNamesMask&(1<<uint(i)) == 0 {
			continue
		}
		if groupIdx < len(namesReply.GroupNameAtoms) {
			km.groupNames[i] = atomNames[namesReply.GroupNameAtoms[groupIdx]]
		}
		groupIdx++
	}

	// Per-key data: name, symbols/groups, auto-repeat.
	for i, sm := range mapReply.KeySyms {
		kc := mapReply.MinKeyCode + Keycode(i)
		key := &Key{keycode: kc}

		if sm.NumGroups > 0 {
			groups := make([]KeyGroup, sm.NumGroups)
			for g := 0; g < sm.NumGroups; g++ {
				typeIdx := int(sm.TypeIndex[g])
				if typeIdx >= len(typesList) {
					return nil, &Error{Op: op, Err: fmt.Errorf("keycode %d group %d references invalid type index %d", kc, g, typeIdx)}
				}
				levels := make([]KeyLevel, sm.Width)
				for l := 0; l < sm.Width; l++ {
					idx := g*sm.Width + l
					var sym Keysym
					if idx < len(sm.Syms) {
						sym = sm.Syms[idx]
					}
					levels[l] = KeyLevel{syms: []Keysym{sym}}
				}
				groups[g] = KeyGroup{keyType: typesList[typeIdx], levels: levels}
			}
			key.groups = groups
		}

		if i < len(namesReply.KeyNames) {
			name := namesReply.KeyNames[i]
			key.name = name
			if name != "" {
				km.keycodeNames[kc] = name
				km.keycodesByName[name] = kc
			}
		}

		byteIdx := int(kc) / 8
		bitIdx := uint(int(kc) % 8)
		if byteIdx < len(controlsReply.PerKeyRepeat) {
			key.repeats = controlsReply.PerKeyRepeat[byteIdx]&(1<<bitIdx) != 0
		}

		km.keys[kc] = key
	}

	// Real modifier map (xkb_symbols `modifier_map`-equivalent).
	for _, mm := range mapReply.ModMap {
		if key, ok := km.keys[mm.Keycode]; ok {
			key.vmodmap |= mm.Mods
		}
	}

	// Key aliases: extra keycodesByName entries pointing at the real key,
	// matching how the text-format parser records `alias` statements.
	for _, alias := range namesReply.Aliases {
		if kc, ok := km.keycodesByName[alias.Real]; ok && alias.Alias != "" {
			km.keycodesByName[alias.Alias] = kc
		}
	}

	return km, nil
}
