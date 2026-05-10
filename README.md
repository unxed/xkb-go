# xkb-go

A pure Go implementation of the XKB (X Keyboard Extension) library, compatible with libxkbcommon.

## Why?

libxkbcommon is the standard library for keyboard handling in Wayland. However, using it from Go requires:

- **CGO bindings**: Breaks cross-compilation, requires C toolchain
- **purego**: Still requires libxkbcommon.so at runtime

xkb-go provides the same functionality in pure Go:

- No CGO, no C dependencies
- Cross-compile to any platform
- Single static binary
- Native Go error handling and types

## Installation

```bash
go get github.com/unxed/xkb-go
```

## Quick Start

```go
package main

import (
    "context"
    "fmt"
    "github.com/unxed/xkb-go"
)

func main() {
    // Create context
    ctx := xkb.NewContext(context.Background(), xkb.ContextNoFlags)

    // Load keymap from Wayland compositor (wl_keyboard.keymap event)
    keymap, err := ctx.NewKeymapFromString(keymapData, xkb.KeymapFormatTextV1)
    if err != nil {
        panic(err)
    }

    // Create state for tracking keyboard
    state := keymap.NewState()

    // Update modifier state (from wl_keyboard.modifiers)
    state.UpdateMask(shiftPressed, 0, capsLockOn, 0, 0, 0)

    // Translate keycode to keysym
    sym := state.KeyGetOneSym(keycode)

    // Get Unicode character
    char := state.KeyGetUTF32(keycode)
    fmt.Printf("Key: %s (%c)\n", xkb.KeysymGetName(sym), char)
}
```

### Loading from RMLVO Names

Build keymaps from system XKB data without a Wayland compositor:

```go
// Load US layout
keymap, err := ctx.NewKeymapFromNames(&xkb.RuleNames{
    Layout: "us",
})

// With variant
keymap, err := ctx.NewKeymapFromNames(&xkb.RuleNames{
    Layout:  "us",
    Variant: "intl",
})

// Full RMLVO specification
keymap, err := ctx.NewKeymapFromNames(&xkb.RuleNames{
    Rules:   "evdev",      // default: "evdev"
    Model:   "pc105",      // default: "pc105"
    Layout:  "us,ru",      // required
    Variant: ",phonetic",  // optional
    Options: "ctrl:nocaps",// optional
})
```

### Compose/Dead Keys

```go
// Load compose table for locale
table, err := ctx.NewComposeTableFromLocale("en_US.UTF-8", xkb.ComposeCompileNoFlags)
composeState := table.NewState(xkb.ComposeStateNoFlags)

// Feed keysyms
composeState.Feed(xkb.KeyDeadAcute)
composeState.Feed(xkb.KeyA)

if composeState.GetStatus() == xkb.ComposeComposed {
    result := composeState.GetUTF8() // "á"
}
```

## Features

- [x] Keymap parsing (XKB text format v1)
- [x] Keyboard state tracking (modifiers, groups)
- [x] Key translation (keycode → keysym → Unicode)
- [x] Compose/dead key sequences
- [x] RMLVO compilation (`NewKeymapFromNames`)
- [x] Keymap serialization (`GetAsString`)
- [x] Full keysym tables (~2500 keysyms)

## XKB Concepts

**Keycode**: Physical key identifier. Linux evdev keycode = scancode + 8.

**Keysym**: Abstract symbol a key produces (e.g., `XKB_KEY_a`, `XKB_KEY_Shift_L`).

**Modifiers**: Keys that modify others. 8 real modifiers: Shift, Lock, Control, Mod1-5.

**Groups**: Different layouts (e.g., Group1=English, Group2=Russian). Max 4.

**Levels**: Different outputs based on modifiers (e.g., Level1=`a`, Level2=`A` with Shift).

## Thread Safety

- `Context`: Safe for concurrent use
- `Keymap`: Immutable after creation, safe to share
- `State`: NOT safe for concurrent use (one per keyboard)
- `ComposeState`: NOT safe for concurrent use

## API Reference

| libxkbcommon | xkb-go |
|--------------|--------|
| `xkb_context_new()` | `xkb.NewContext(ctx, flags)` |
| `xkb_context_unref()` | (garbage collected) |
| `xkb_keymap_new_from_string()` | `ctx.NewKeymapFromString()` |
| `xkb_keymap_new_from_file()` | `ctx.NewKeymapFromFile()` |
| `xkb_keymap_new_from_names()` | `ctx.NewKeymapFromNames()` |
| `xkb_keymap_get_as_string()` | `keymap.GetAsString()` |
| `xkb_state_new()` | `keymap.NewState()` |
| `xkb_state_key_get_one_sym()` | `state.KeyGetOneSym()` |
| `xkb_state_key_get_utf8()` | `state.KeyGetUTF8()` |
| `xkb_state_key_get_utf32()` | `state.KeyGetUTF32()` |
| `xkb_state_update_mask()` | `state.UpdateMask()` |
| `xkb_state_update_key()` | `state.UpdateKey()` |
| `xkb_keysym_get_name()` | `xkb.KeysymGetName()` |
| `xkb_keysym_from_name()` | `xkb.KeysymFromName()` |
| `xkb_keysym_to_utf32()` | `xkb.KeysymToUTF32()` |
| `xkb_compose_table_new_from_locale()` | `ctx.NewComposeTableFromLocale()` |
| `xkb_compose_state_feed()` | `composeState.Feed()` |
| `xkb_compose_state_get_status()` | `composeState.GetStatus()` |
| `xkb_compose_state_get_utf8()` | `composeState.GetUTF8()` |

## Documentation

- [Architecture](docs/architecture.md) - Data structures and concepts
- [References](docs/references.md) - XKB specifications and resources

## License

MIT License - see [LICENSE](LICENSE)

## Related

- [libxkbcommon](https://xkbcommon.org/) - The C reference implementation
- [xkbcommon docs](https://xkbcommon.org/doc/current/) - XKB documentation
