package xkb_test

import (
	"context"
	"fmt"

	"github.com/unxed/xkb-go"
)

func ExampleNewContext() {
	// Create a context with default settings
	ctx := xkb.NewContext(context.Background(), xkb.ContextNoFlags)

	// The context manages include paths and logging
	paths := ctx.IncludePaths()
	fmt.Printf("Include paths: %d\n", len(paths))
	// Output: Include paths: 1
}

func ExampleContext_NewKeymapFromString() {
	ctx := xkb.NewContext(context.Background(), xkb.ContextNoFlags)

	// In real usage, this would be the keymap string from Wayland
	// received via wl_keyboard.keymap event
	keymapData := []byte(`xkb_keymap { ... }`) // Simplified

	_, err := ctx.NewKeymapFromString(keymapData, xkb.KeymapFormatTextV1)
	if err != nil {
		// Handle error - parser not yet implemented
		fmt.Println("Keymap parsing not yet implemented")
	}
	// Output: Keymap parsing not yet implemented
}

func ExampleKeysymToUTF32() {
	// Convert keysyms to Unicode codepoints
	fmt.Printf("a -> %c\n", xkb.KeysymToUTF32(xkb.Keysym('a')))
	fmt.Printf("A -> %c\n", xkb.KeysymToUTF32(xkb.Keysym('A')))

	// Latin-1 characters
	fmt.Printf("0xe4 -> %c\n", xkb.KeysymToUTF32(xkb.Keysym(0xe4))) // ä

	// Function keys don't produce characters
	r := xkb.KeysymToUTF32(xkb.KeyReturn)
	fmt.Printf("Return -> %d (no char)\n", r)
	// Output:
	// a -> a
	// A -> A
	// 0xe4 -> ä
	// Return -> 0 (no char)
}

func ExampleKeysymGetName() {
	// Get the name of a keysym
	fmt.Println(xkb.KeysymGetName(xkb.KeyReturn))
	fmt.Println(xkb.KeysymGetName(xkb.KeyShiftL))
	fmt.Println(xkb.KeysymGetName(xkb.KeyDeadAcute))
	fmt.Println(xkb.KeysymGetName(xkb.Keysym('a')))
	// Output:
	// Return
	// Shift_L
	// dead_acute
	// a
}

func ExampleKeysymFromName() {
	// Look up keysym by name
	ks := xkb.KeysymFromName("Return", xkb.KeysymNameNoFlags)
	fmt.Printf("Return = 0x%04x\n", ks)

	ks = xkb.KeysymFromName("Shift_L", xkb.KeysymNameNoFlags)
	fmt.Printf("Shift_L = 0x%04x\n", ks)

	ks = xkb.KeysymFromName("nonexistent", xkb.KeysymNameNoFlags)
	fmt.Printf("nonexistent = 0x%04x (NoSymbol)\n", ks)
	// Output:
	// Return = 0xff0d
	// Shift_L = 0xffe1
	// nonexistent = 0x0000 (NoSymbol)
}

func ExampleKeymap_NewState() {
	// Create a test keymap (in real usage, parse from string)
	km := xkb.TestKeymap()

	// Create state for tracking keyboard
	state := km.NewState()

	// Simulate pressing 'a' with no modifiers
	state.UpdateMask(0, 0, 0, 0, 0, 0)
	sym := state.KeyGetOneSym(38) // keycode 38 = 'a' on US keyboard
	fmt.Printf("No mods: %s\n", xkb.KeysymGetName(sym))

	// Simulate pressing 'a' with Shift held
	state.UpdateMask(xkb.ModShift, 0, 0, 0, 0, 0)
	sym = state.KeyGetOneSym(38)
	fmt.Printf("With Shift: %s\n", xkb.KeysymGetName(sym))

	// Simulate pressing 'a' with Caps Lock on
	state.UpdateMask(0, 0, xkb.ModLock, 0, 0, 0)
	sym = state.KeyGetOneSym(38)
	fmt.Printf("With Caps Lock: %s\n", xkb.KeysymGetName(sym))
	// Output:
	// No mods: a
	// With Shift: A
	// With Caps Lock: A
}

func ExampleState_KeyGetUTF8() {
	km := xkb.TestKeymap()
	state := km.NewState()

	// Get the character for a key
	char := state.KeyGetUTF8(38) // 'a' key
	fmt.Printf("Key 38 = %q\n", char)

	// With Shift
	state.UpdateMask(xkb.ModShift, 0, 0, 0, 0, 0)
	char = state.KeyGetUTF8(38)
	fmt.Printf("Key 38 + Shift = %q\n", char)
	// Output:
	// Key 38 = "a"
	// Key 38 + Shift = "A"
}

func ExampleComposeTable_NewState() {
	// Create a test compose table
	ct := xkb.TestComposeTable()
	cs := ct.NewState(xkb.ComposeStateNoFlags)

	// Simulate dead_acute + a = á
	cs.Feed(xkb.KeyDeadAcute)
	fmt.Printf("After dead_acute: %v\n", cs.GetStatus())

	cs.Feed(xkb.Keysym('a'))
	fmt.Printf("After 'a': %v\n", cs.GetStatus())
	fmt.Printf("Result: %s\n", cs.GetUTF8())
	// Output:
	// After dead_acute: 1
	// After 'a': 2
	// Result: á
}

func ExampleComposeState_Reset() {
	ct := xkb.TestComposeTable()
	cs := ct.NewState(xkb.ComposeStateNoFlags)

	// Start a compose sequence
	cs.Feed(xkb.KeyDeadAcute)

	// User presses Escape - cancel the sequence
	cs.Reset()

	fmt.Printf("Status after reset: %v (ComposeNothing)\n", cs.GetStatus())
	// Output: Status after reset: 0 (ComposeNothing)
}

func ExampleKeysymIsModifier() {
	// Check if keysyms are modifiers
	fmt.Printf("Shift_L is modifier: %v\n", xkb.KeysymIsModifier(xkb.KeyShiftL))
	fmt.Printf("Control_L is modifier: %v\n", xkb.KeysymIsModifier(xkb.KeyControlL))
	fmt.Printf("Return is modifier: %v\n", xkb.KeysymIsModifier(xkb.KeyReturn))
	fmt.Printf("'a' is modifier: %v\n", xkb.KeysymIsModifier(xkb.Keysym('a')))
	// Output:
	// Shift_L is modifier: true
	// Control_L is modifier: true
	// Return is modifier: false
	// 'a' is modifier: false
}
