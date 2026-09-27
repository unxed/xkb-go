// Package x11 builds a [xkb.Keymap] by querying the X Keyboard Extension
// (XKB) of a live X11 connection, without CGO, libxkbcommon, or shelling
// out to xkbcomp.
//
// It is a thin transport layer on top of github.com/jezek/xgb: it sends the
// GetMap, GetNames and GetControls XKB requests, resolves the ATOMs they
// reference via the core X11 GetAtomName request, and hands the raw reply
// bytes and resolved names to the pure decoder in the root xkb-go package
// (see NewKeymapFromX11Replies there). This package is kept separate from
// github.com/unxed/xkb-go itself so that consumers who only need
// Wayland/text-format keymap support are not forced to pull in an X11
// client library.
package x11

import (
	"fmt"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"

	xkb "github.com/unxed/xkb-go"
)

// extensionName is the name the X server registers the X Keyboard
// Extension under.
const extensionName = "XKEYBOARD"

// XKB request opcodes (the extension's minor opcodes), from the xkb.xml
// protocol description shipped with xcb-proto.
const (
	opUseExtension = 0
	opGetControls  = 6
	opGetMap       = 8
	opGetNames     = 17
)

// UseCoreKbd is the special XKB device ID that refers to the X server's
// core keyboard device. Pass it as deviceID to [NewKeymapFromX11Device]
// when a specific input device is not required.
const UseCoreKbd int32 = 0x0100

// NewKeymapFromX11Device builds an [xkb.Keymap] by querying the XKB
// extension of the X server conn is connected to, for the given input
// device (pass [UseCoreKbd] for the core keyboard).
//
// This is a pure Go equivalent of libxkbcommon's
// xkb_x11_keymap_new_from_device. conn must already be connected (see
// [xgb.NewConn]); the XKEYBOARD extension is initialized on it
// automatically if needed.
//
// See the package-level xkb-go documentation of NewKeymapFromX11Replies
// for exactly what subset of GetMap/GetNames/GetControls is requested and
// why (in short: everything [xkb.Keymap] represents, and nothing the X
// server has already resolved for us, such as indicators, key actions, or
// the compat map).
func NewKeymapFromX11Device(ctx *xkb.Context, conn *xgb.Conn, deviceID int32) (*xkb.Keymap, error) {
	if deviceID < 0 || deviceID > 0xffff {
		return nil, fmt.Errorf("x11: invalid XKB device ID %d", deviceID)
	}
	dev := uint16(deviceID)

	major, err := ensureXKBExtension(conn)
	if err != nil {
		return nil, fmt.Errorf("x11: %w", err)
	}

	// Pipeline all three requests before blocking on any reply, so only
	// one network round trip is needed for the keymap data itself (a
	// second round trip, below, resolves ATOM names).
	mapCookie := conn.NewCookie(true, true)
	conn.NewRequest(getMapRequest(major, dev), mapCookie)

	namesCookie := conn.NewCookie(true, true)
	conn.NewRequest(getNamesRequest(major, dev), namesCookie)

	controlsCookie := conn.NewCookie(true, true)
	conn.NewRequest(getControlsRequest(major, dev), controlsCookie)

	mapBuf, err := mapCookie.Reply()
	if err != nil {
		return nil, fmt.Errorf("x11: GetMap: %w", err)
	}
	mapReply, err := xkb.ParseX11GetMapReply(mapBuf)
	if err != nil {
		return nil, fmt.Errorf("x11: %w", err)
	}

	namesBuf, err := namesCookie.Reply()
	if err != nil {
		return nil, fmt.Errorf("x11: GetNames: %w", err)
	}
	namesReply, err := xkb.ParseX11GetNamesReply(namesBuf)
	if err != nil {
		return nil, fmt.Errorf("x11: %w", err)
	}

	controlsBuf, err := controlsCookie.Reply()
	if err != nil {
		return nil, fmt.Errorf("x11: GetControls: %w", err)
	}
	controlsReply, err := xkb.ParseX11GetControlsReply(controlsBuf)
	if err != nil {
		return nil, fmt.Errorf("x11: %w", err)
	}

	atomNames, err := resolveAtomNames(conn, collectAtoms(namesReply))
	if err != nil {
		return nil, fmt.Errorf("x11: %w", err)
	}

	km, err := xkb.NewKeymapFromX11Replies(ctx, mapReply, namesReply, controlsReply, atomNames)
	if err != nil {
		return nil, fmt.Errorf("x11: %w", err)
	}
	return km, nil
}

// ensureXKBExtension registers the XKEYBOARD extension on conn (if not
// already done), negotiates a protocol version as XKB clients are
// required to, and returns the extension's major opcode.
func ensureXKBExtension(conn *xgb.Conn) (byte, error) {
	conn.ExtLock.RLock()
	major, ok := conn.Extensions[extensionName]
	conn.ExtLock.RUnlock()
	if ok {
		return major, nil
	}

	reply, err := xproto.QueryExtension(conn, uint16(len(extensionName)), extensionName).Reply()
	if err != nil {
		return 0, fmt.Errorf("QueryExtension(%s): %w", extensionName, err)
	}
	if reply == nil || !reply.Present {
		return 0, fmt.Errorf("X server does not support the %s (XKB) extension", extensionName)
	}

	conn.ExtLock.Lock()
	conn.Extensions[extensionName] = reply.MajorOpcode
	conn.ExtLock.Unlock()

	// XKB clients must call UseExtension before using the extension; we
	// don't need a specific version, so request the earliest one (1.0)
	// purely to complete the handshake the protocol requires.
	cookie := conn.NewCookie(true, true)
	conn.NewRequest(useExtensionRequest(reply.MajorOpcode, 1, 0), cookie)
	buf, err := cookie.Reply()
	if err != nil {
		return 0, fmt.Errorf("UseExtension: %w", err)
	}
	if len(buf) < 2 || buf[1] == 0 {
		return 0, fmt.Errorf("X server does not support XKB version 1.0")
	}

	return reply.MajorOpcode, nil
}

// useExtensionRequest builds a raw XKB UseExtension request.
func useExtensionRequest(major byte, wantedMajor, wantedMinor uint16) []byte {
	buf := make([]byte, 8)
	buf[0] = major
	buf[1] = opUseExtension
	xgb.Put16(buf[2:], uint16(len(buf)/4))
	xgb.Put16(buf[4:], wantedMajor)
	xgb.Put16(buf[6:], wantedMinor)
	return buf
}

// getMapRequest builds a raw XKB GetMap request asking for everything
// xkb.ParseX11GetMapReply understands, for the full keycode range.
func getMapRequest(major byte, deviceSpec uint16) []byte {
	buf := make([]byte, 28)
	buf[0] = major
	buf[1] = opGetMap
	xgb.Put16(buf[2:], uint16(len(buf)/4))
	xgb.Put16(buf[4:], deviceSpec)
	xgb.Put16(buf[6:], uint16(xkb.X11GetMapWanted)) // full
	// partial and all first*/n* fields are left zero, meaning "the
	// complete range" for every section named in `full`.
	return buf
}

// getNamesRequest builds a raw XKB GetNames request asking for everything
// xkb.ParseX11GetNamesReply understands.
func getNamesRequest(major byte, deviceSpec uint16) []byte {
	buf := make([]byte, 12)
	buf[0] = major
	buf[1] = opGetNames
	xgb.Put16(buf[2:], uint16(len(buf)/4))
	xgb.Put16(buf[4:], deviceSpec)
	xgb.Put32(buf[8:], uint32(xkb.X11GetNamesWanted))
	return buf
}

// getControlsRequest builds a raw XKB GetControls request.
func getControlsRequest(major byte, deviceSpec uint16) []byte {
	buf := make([]byte, 8)
	buf[0] = major
	buf[1] = opGetControls
	xgb.Put16(buf[2:], uint16(len(buf)/4))
	xgb.Put16(buf[4:], deviceSpec)
	return buf
}

// collectAtoms gathers the distinct, non-zero ATOM IDs referenced by a
// parsed GetNames reply, in no particular order.
func collectAtoms(names *xkb.X11NamesReply) []uint32 {
	seen := make(map[uint32]bool)
	var atoms []uint32
	add := func(a uint32) {
		if a != 0 && !seen[a] {
			seen[a] = true
			atoms = append(atoms, a)
		}
	}
	for _, a := range names.TypeNameAtoms {
		add(a)
	}
	for _, a := range names.VirtualModNameAtoms {
		add(a)
	}
	for _, a := range names.GroupNameAtoms {
		add(a)
	}
	return atoms
}

// resolveAtomNames resolves a set of X11 ATOM IDs to their string names
// via the core X11 GetAtomName request, pipelining all of the requests
// into a single round trip.
func resolveAtomNames(conn *xgb.Conn, atoms []uint32) (map[uint32]string, error) {
	result := make(map[uint32]string, len(atoms))
	if len(atoms) == 0 {
		return result, nil
	}

	cookies := make([]xproto.GetAtomNameCookie, len(atoms))
	for i, atom := range atoms {
		cookies[i] = xproto.GetAtomName(conn, xproto.Atom(atom))
	}
	for i, cookie := range cookies {
		reply, err := cookie.Reply()
		if err != nil {
			return nil, fmt.Errorf("GetAtomName(%d): %w", atoms[i], err)
		}
		result[atoms[i]] = reply.Name
	}
	return result, nil
}
