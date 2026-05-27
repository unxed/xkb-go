package xkb

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// ContextFlags controls [Context] behavior.
type ContextFlags uint32

const (
	// ContextNoFlags is the default context flags.
	ContextNoFlags ContextFlags = 0

	// ContextNoDefaultIncludes prevents adding default include paths.
	// By default, the context includes system XKB data paths like /usr/share/X11/xkb.
	ContextNoDefaultIncludes ContextFlags = 1 << 0

	// ContextNoEnvironmentNames prevents reading RMLVO names from environment variables.
	// Affects XKB_DEFAULT_RULES, XKB_DEFAULT_MODEL, XKB_DEFAULT_LAYOUT, etc.
	ContextNoEnvironmentNames ContextFlags = 1 << 1
)

// Context is the top-level xkb object that holds shared configuration.
//
// A Context manages include paths for loading XKB data files and provides
// factory methods for creating [Keymap] and [ComposeTable] instances.
// Context is safe for concurrent use.
//
// Create a Context with [NewContext], then use methods like [Context.NewKeymapFromString]
// or [Context.NewKeymapFromNames] to create keymaps.
type Context struct {
	ctx          context.Context
	mu           sync.RWMutex
	flags        ContextFlags
	logger       *slog.Logger
	includePaths []string
}

// NewContext creates a new xkb context with the given flags.
//
// The provided [context.Context] is used for cancellation and logging.
// Pass [ContextNoFlags] for default behavior, which includes adding
// standard system XKB data paths.
func NewContext(ctx context.Context, flags ContextFlags) *Context {
	c := &Context{
		ctx:    ctx,
		flags:  flags,
		logger: slog.Default(),
	}

	if flags&ContextNoDefaultIncludes == 0 {
		c.addDefaultIncludePaths()
	}

	return c
}

// Context returns the [context.Context] associated with this xkb Context.
func (c *Context) Context() context.Context {
	return c.ctx
}

// addDefaultIncludePaths adds the standard XKB data directories.
func (c *Context) addDefaultIncludePaths() {
	// User XKB directory
	if home, err := os.UserHomeDir(); err == nil {
		userXKB := filepath.Join(home, ".xkb")
		if info, err := os.Stat(userXKB); err == nil && info.IsDir() {
			c.includePaths = append(c.includePaths, userXKB)
		}
	}

	// XDG config directory
	if xdgConfig := os.Getenv("XDG_CONFIG_HOME"); xdgConfig != "" {
		xkbDir := filepath.Join(xdgConfig, "xkb")
		if info, err := os.Stat(xkbDir); err == nil && info.IsDir() {
			c.includePaths = append(c.includePaths, xkbDir)
		}
	}

	// System XKB data directories
	systemPaths := []string{
		"/usr/share/X11/xkb",
		"/usr/local/share/X11/xkb",
		// macOS include paths
		"/opt/X11/share/X11/xkb",      // XQuartz
		"/opt/local/share/X11/xkb",    // MacPorts
		"/opt/homebrew/share/X11/xkb", // Homebrew (Apple Silicon)
	}

	if runtime.GOOS == "windows" {
		drive := os.Getenv("SystemDrive")
		if drive == "" {
			drive = "C:"
		}
		windowsPaths := []string{
			filepath.Join(drive, "Program Files", "VcXsrv", "xkb"),
			filepath.Join(drive, "Program Files (x86)", "VcXsrv", "xkb"),
			filepath.Join(drive, "Program Files", "Xming", "xkb"),
			filepath.Join(drive, "Program Files (x86)", "Xming", "xkb"),
			filepath.Join(drive, "msys64", "usr", "share", "X11", "xkb"),
			filepath.Join(drive, "cygwin64", "usr", "share", "X11", "xkb"),
			filepath.Join(drive, "cygwin", "usr", "share", "X11", "xkb"),
		}
		systemPaths = append(systemPaths, windowsPaths...)
	}

	for _, p := range systemPaths {
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			c.includePaths = append(c.includePaths, p)
		}
	}
}

// SetLogger sets the logger for this context.
//
// If logger is nil, a no-op logger is used. The logger is called
// for warnings and errors during keymap parsing and compilation.
func (c *Context) SetLogger(logger *slog.Logger) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if logger == nil {
		// Create a no-op logger
		logger = slog.New(slog.NewTextHandler(nil, nil))
	}
	c.logger = logger
}

// Logger returns the current logger.
func (c *Context) Logger() *slog.Logger {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.logger
}

// log is a convenience method for logging.
func (c *Context) log(level slog.Level, msg string, args ...any) {
	c.mu.RLock()
	logger := c.logger
	c.mu.RUnlock()

	if logger != nil {
		logger.Log(c.ctx, level, msg, args...)
	}
}

// IncludePaths returns a copy of the current include paths.
//
// These paths are searched when loading XKB data files for [Context.NewKeymapFromNames].
func (c *Context) IncludePaths() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	paths := make([]string, len(c.includePaths))
	copy(paths, c.includePaths)
	return paths
}

// AppendIncludePath adds a path to the end of the include path list.
//
// Later paths have lower priority when resolving includes.
// See also [Context.PrependIncludePath] to add high-priority paths.
func (c *Context) AppendIncludePath(path string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.includePaths = append(c.includePaths, path)
}

// PrependIncludePath adds a path to the front of the include path list.
//
// Earlier paths have higher priority when resolving includes.
// See also [Context.AppendIncludePath] to add low-priority paths.
func (c *Context) PrependIncludePath(path string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.includePaths = append([]string{path}, c.includePaths...)
}

// ClearIncludePaths removes all include paths.
func (c *Context) ClearIncludePaths() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.includePaths = nil
}

// NumIncludePaths returns the number of include paths.
func (c *Context) NumIncludePaths() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.includePaths)
}

// Flags returns the [ContextFlags] this context was created with.
func (c *Context) Flags() ContextFlags {
	return c.flags
}

// resolvePath looks for a file in the include paths.
// Returns the full path if found, empty string otherwise.
func (c *Context) resolvePath(filename string) string {
	c.mu.RLock()
	paths := c.includePaths
	c.mu.RUnlock()

	for _, base := range paths {
		full := filepath.Join(base, filename)
		if _, err := os.Stat(full); err == nil {
			return full
		}
	}
	return ""
}

// NewKeymapFromString parses a keymap from XKB text format.
//
// This is the primary method for Wayland clients, which receive
// the complete keymap as a string from the compositor via wl_keyboard.keymap.
//
// The format parameter must be [KeymapFormatTextV1].
// Returns a [Keymap] that can be used to create a [State] for key translation.
func (c *Context) NewKeymapFromString(text []byte, format KeymapFormat) (*Keymap, error) {
	if format != KeymapFormatTextV1 {
		return nil, &Error{Op: "NewKeymapFromString", Err: ErrUnsupportedFormat}
	}

	parser := NewParser(text)
	keymap, err := parser.Parse()
	if err != nil {
		return nil, &Error{Op: "NewKeymapFromString", Err: err}
	}

	keymap.ctx = c
	return keymap, nil
}

// NewKeymapFromFile loads and parses a keymap from a file.
//
// The format parameter must be [KeymapFormatTextV1].
// See also [Context.NewKeymapFromString] for parsing from a byte slice.
func (c *Context) NewKeymapFromFile(path string, format KeymapFormat) (*Keymap, error) {
	if format != KeymapFormatTextV1 {
		return nil, &Error{Op: "NewKeymapFromFile", Err: ErrUnsupportedFormat}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, &Error{Op: "NewKeymapFromFile", Path: path, Err: err}
	}

	keymap, err := c.NewKeymapFromString(data, format)
	if err != nil {
		// Unwrap and rewrap with path info
		if e, ok := err.(*Error); ok {
			return nil, &Error{Op: "NewKeymapFromFile", Path: path, Err: e.Err}
		}
		return nil, &Error{Op: "NewKeymapFromFile", Path: path, Err: err}
	}

	return keymap, nil
}

// NewKeymapFromNames builds a keymap from RMLVO names.
//
// This looks up the rules file and assembles keymap components from
// the system XKB data directories. Useful for building keymaps without
// a Wayland compositor.
//
// If names is nil, default values are used (Rules="evdev", Model="pc105", Layout="us").
// Empty fields in [RuleNames] fall back to these defaults.
func (c *Context) NewKeymapFromNames(names *RuleNames) (*Keymap, error) {
	if names == nil {
		names = &RuleNames{}
	}

	keymap, err := c.compileKeymap(names)
	if err != nil {
		return nil, &Error{Op: "NewKeymapFromNames", Err: err}
	}

	return keymap, nil
}

// NewComposeTableFromLocale loads a compose table for the given locale.
//
// The locale string should be in the form "language_TERRITORY.encoding"
// (e.g., "en_US.UTF-8"). If empty, the locale is read from environment
// variables (LC_ALL, LC_CTYPE, LANG).
//
// The compose table is searched for in:
//  1. $XCOMPOSEFILE environment variable
//  2. ~/.XCompose
//  3. System locale compose file (/usr/share/X11/locale/<locale>/Compose)
//
// Returns a [ComposeTable] that can be used to create a [ComposeState].
func (c *Context) NewComposeTableFromLocale(locale string, flags ComposeCompileFlags) (*ComposeTable, error) {
	// Normalize locale
	if locale == "" {
		locale = os.Getenv("LC_ALL")
		if locale == "" {
			locale = os.Getenv("LC_CTYPE")
			if locale == "" {
				locale = os.Getenv("LANG")
				if locale == "" {
					locale = "C"
				}
			}
		}
	}

	// 1. Check XCOMPOSEFILE environment variable
	if composePath := os.Getenv("XCOMPOSEFILE"); composePath != "" {
		if _, err := os.Stat(composePath); err == nil {
			return c.NewComposeTableFromFile(composePath, locale, flags)
		}
	}

	// 2. Check ~/.XCompose
	if home, err := os.UserHomeDir(); err == nil {
		userCompose := filepath.Join(home, ".XCompose")
		if _, err := os.Stat(userCompose); err == nil {
			return c.NewComposeTableFromFile(userCompose, locale, flags)
		}
	}

	// 3. Find system compose file for locale
	composePath := c.findComposeFileForLocale(locale)
	if composePath == "" {
		return nil, &Error{Op: "NewComposeTableFromLocale", Err: fmt.Errorf("no compose file found for locale %s", locale)}
	}

	return c.NewComposeTableFromFile(composePath, locale, flags)
}

// NewComposeTableFromFile loads a compose table from a specific file.
//
// The locale parameter is used for resolving %L and similar substitutions
// in include directives. The flags parameter is currently unused.
//
// See also [Context.NewComposeTableFromLocale] for automatic locale detection.
func (c *Context) NewComposeTableFromFile(path string, locale string, flags ComposeCompileFlags) (*ComposeTable, error) {
	parser := newComposeParser(locale, c.IncludePaths())
	if err := parser.parseFile(path); err != nil {
		return nil, &Error{Op: "NewComposeTableFromFile", Err: err}
	}

	return parser.buildComposeTable(), nil
}

// findComposeFileForLocale finds the system compose file for a locale.
func (c *Context) findComposeFileForLocale(locale string) string {
	// Common locale directories
	localeDirs := []string{
		"/usr/share/X11/locale",
		"/usr/local/share/X11/locale",
	}

	// Try direct path first (locale/Compose)
	for _, baseDir := range localeDirs {
		composePath := filepath.Join(baseDir, locale, "Compose")
		if _, err := os.Stat(composePath); err == nil {
			return composePath
		}
	}

	// Try looking up in compose.dir
	for _, baseDir := range localeDirs {
		composeDirPath := filepath.Join(baseDir, "compose.dir")
		mapping := c.parseComposeDir(composeDirPath)
		if relPath, ok := mapping[locale]; ok {
			composePath := filepath.Join(baseDir, relPath)
			if _, err := os.Stat(composePath); err == nil {
				return composePath
			}
		}

		// Try without encoding suffix (e.g., "en_US" from "en_US.UTF-8")
		localeParts := strings.Split(locale, ".")
		if len(localeParts) > 1 {
			baseLocale := localeParts[0]
			if relPath, ok := mapping[baseLocale]; ok {
				composePath := filepath.Join(baseDir, relPath)
				if _, err := os.Stat(composePath); err == nil {
					return composePath
				}
			}
		}
	}

	// Fallback: try en_US.UTF-8
	if locale != "en_US.UTF-8" {
		for _, baseDir := range localeDirs {
			composePath := filepath.Join(baseDir, "en_US.UTF-8", "Compose")
			if _, err := os.Stat(composePath); err == nil {
				return composePath
			}
		}
	}

	return ""
}

// parseComposeDir parses a compose.dir file and returns a mapping of locale to compose file path.
func (c *Context) parseComposeDir(path string) map[string]string {
	result := make(map[string]string)

	file, err := os.Open(path)
	if err != nil {
		return result
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Format: "compose_file locale"
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			result[fields[1]] = fields[0]
		}
	}

	return result
}
