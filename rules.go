package xkb

import (
	"bufio"
    "strconv"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// kcCGST holds the resolved component names for building a keymap.
type kcCGST struct {
	Keycodes string
	Types    string
	Compat   string
	Symbols  string
	Geometry string
}

// rulesFile represents a parsed XKB rules file.
type rulesFile struct {
	variables map[string][]string // $varname -> values
	rules     []rule
}

// rule represents a single rule mapping.
type rule struct {
	model     string
	layouts   [4]string
	variants  [4]string
	option    string
	component string
	value     string
}

// parseRulesFile parses an XKB rules file.
func parseRulesFile(path string) (*rulesFile, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	rf := &rulesFile{
		variables: make(map[string][]string),
	}

	scanner := bufio.NewScanner(file)
	var currentHeader []string
	var currentComponent string

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}

		// Variable definition: ! $varname = value1 value2 ...
		if strings.HasPrefix(line, "!") && strings.Contains(line, "$") && strings.Contains(line, "=") {
			rf.parseVariable(line)
			continue
		}

		// Rule header: ! model layout = symbols
		if strings.HasPrefix(line, "!") {
			currentHeader, currentComponent = rf.parseRuleHeader(line)
			continue
		}

		// Rule body: pattern = value
		if len(currentHeader) > 0 && currentComponent != "" && strings.Contains(line, "=") {
			rf.parseRuleBody(line, currentHeader, currentComponent)
		}
	}

	return rf, scanner.Err()
}

// parseVariable parses a variable definition line.
func (rf *rulesFile) parseVariable(line string) {
	// Format: ! $varname = value1 value2 ...
	line = strings.TrimPrefix(line, "!")
	line = strings.TrimSpace(line)

	parts := strings.SplitN(line, "=", 2)
	if len(parts) != 2 {
		return
	}

	name := strings.TrimSpace(parts[0])
	if !strings.HasPrefix(name, "$") {
		return
	}
	name = name[1:] // Remove $

	values := strings.Fields(parts[1])
	// Handle continuation with backslash (already concatenated by scanner)
	rf.variables[name] = values
}

// parseRuleHeader parses a rule header line.
func (rf *rulesFile) parseRuleHeader(line string) ([]string, string) {
	// Format: ! model layout variant = symbols
	line = strings.TrimPrefix(line, "!")
	line = strings.TrimSpace(line)

	parts := strings.SplitN(line, "=", 2)
	if len(parts) != 2 {
		return nil, ""
	}

	inputs := strings.Fields(parts[0])
	component := strings.TrimSpace(parts[1])

	return inputs, component
}

// parseRuleBody parses a rule body line.
func (rf *rulesFile) parseRuleBody(line string, header []string, component string) {
	parts := strings.SplitN(line, "=", 2)
	if len(parts) != 2 {
		return
	}

	patterns := strings.Fields(parts[0])
	value := strings.TrimSpace(parts[1])

	// Build rule from patterns matching header
	r := rule{
		component: component,
		value:     value,
	}

	for i, pattern := range patterns {
		if i >= len(header) { break }
		switch header[i] {
		case "model": r.model = pattern
		case "layout", "layout[1]": r.layouts[0] = pattern
		case "layout[2]": r.layouts[1] = pattern
		case "layout[3]": r.layouts[2] = pattern
		case "layout[4]": r.layouts[3] = pattern
		case "variant", "variant[1]": r.variants[0] = pattern
		case "variant[2]": r.variants[1] = pattern
		case "variant[3]": r.variants[2] = pattern
		case "variant[4]": r.variants[3] = pattern
		case "option": r.option = pattern
		}
	}

	rf.rules = append(rf.rules, r)
}

// resolve looks up components for the given RMLVO names.
func (rf *rulesFile) resolve(names *RuleNames) *kcCGST {
	result := &kcCGST{}

	// Track already applied values to avoid duplicates
	appliedKeycodes := make(map[string]bool)
	appliedTypes := make(map[string]bool)
	appliedCompat := make(map[string]bool)
	appliedSymbols := make(map[string]bool)
	appliedGeometry := make(map[string]bool)

	for _, r := range rf.rules {
		if !rf.ruleMatches(r, names) {
			continue
		}

		value := rf.substituteValue(r.value, names)


		// Skip empty values
		if value == "" || value == "+" {
			continue
		}

		// Clean up leading/trailing + signs
		value = strings.Trim(value, "+")

		switch r.component {
		case "keycodes":
			if !appliedKeycodes[value] {
				appliedKeycodes[value] = true
				if result.Keycodes == "" {
					result.Keycodes = value
				} else {
					result.Keycodes += "+" + value
				}
			}
		case "types":
			if !appliedTypes[value] {
				appliedTypes[value] = true
				if result.Types == "" {
					result.Types = value
				} else {
					result.Types += "+" + value
				}
			}
		case "compat", "compatibility":
			if !appliedCompat[value] {
				appliedCompat[value] = true
				if result.Compat == "" {
					result.Compat = value
				} else {
					result.Compat += "+" + value
				}
			}
		case "symbols":
			if !appliedSymbols[value] {
				appliedSymbols[value] = true
				if result.Symbols == "" {
					result.Symbols = value
				} else {
					result.Symbols += "+" + value
				}
			}
		case "geometry":
			if !appliedGeometry[value] {
				appliedGeometry[value] = true
				if result.Geometry == "" {
					result.Geometry = value
				} else {
					result.Geometry += "+" + value
				}
			}
		}
	}

	return result
}

// ruleMatches checks if a rule matches the given RMLVO names.
func (rf *rulesFile) ruleMatches(r rule, names *RuleNames) bool {
	if r.model != "" && !rf.patternMatches(r.model, names.Model) { return false }

	layouts := strings.Split(names.Layout, ",")
	for i := 0; i < 4; i++ {
		if r.layouts[i] != "" {
			val := getAt(layouts, i)
			if val == "" || !rf.patternMatches(r.layouts[i], val) { return false }
		}
	}

	variants := strings.Split(names.Variant, ",")
	for i := 0; i < 4; i++ {
		if r.variants[i] != "" {
			val := getAt(variants, i)
			if val == "" || !rf.patternMatches(r.variants[i], val) { return false }
		}
	}

	if r.option != "" {
		if names.Options == "" || !rf.optionMatches(r.option, names.Options) { return false }
	}
	return true
}

func getAt(arr[]string, idx int) string {
	if idx < len(arr) { return strings.TrimSpace(arr[idx]) }
	return ""
}

// optionMatches checks if an option pattern matches the options string.
func (rf *rulesFile) optionMatches(pattern, options string) bool {
	// Options is comma-separated list
	for _, opt := range strings.Split(options, ",") {
		opt = strings.TrimSpace(opt)
		if rf.patternMatches(pattern, opt) {
			return true
		}
	}
	return false
}

// patternMatches checks if a pattern matches a value.
func (rf *rulesFile) patternMatches(pattern, value string) bool {
	// Wildcard matches anything
	if pattern == "*" {
		return true
	}

	// Variable reference
	if strings.HasPrefix(pattern, "$") {
		varName := pattern[1:]
		if values, ok := rf.variables[varName]; ok {
			for _, v := range values {
				if v == value {
					return true
				}
			}
			return false
		}
	}

	// Direct match
	return pattern == value
}

// substituteValue replaces placeholders in a value string.
func (rf *rulesFile) substituteValue(value string, names *RuleNames) string {
	layouts := strings.Split(names.Layout, ",")
	variants := strings.Split(names.Variant, ",")
	l := func(idx int) string { return getAt(layouts, idx) }
	v := func(idx int) string { return getAt(variants, idx) }

	value = strings.ReplaceAll(value, "%m", names.Model)

	if l(3) != "" { value = strings.ReplaceAll(value, "%l[4]", l(3)+":4") }
	if l(2) != "" { value = strings.ReplaceAll(value, "%l[3]", l(2)+":3") }
	if l(1) != "" { value = strings.ReplaceAll(value, "%l[2]", l(1)+":2") }
	value = strings.ReplaceAll(value, "%l[1]", l(0))
	value = strings.ReplaceAll(value, "%l", l(0))

	rv := func(val, ph1, ph2, varVal string) string {
		if varVal != "" {
			val = strings.ReplaceAll(val, ph1, "("+varVal+")")
			val = strings.ReplaceAll(val, ph2, varVal)
		} else {
			val = strings.ReplaceAll(val, ph1, ""); val = strings.ReplaceAll(val, ph2, "")
		}
		return val
	}

	value = rv(value, "%(v[4])", "%v[4]", v(3))
	value = rv(value, "%(v[3])", "%v[3]", v(2))
	value = rv(value, "%(v[2])", "%v[2]", v(1))
	value = rv(value, "%(v[1])", "%v[1]", v(0))
	value = rv(value, "%(v)", "%v", v(0))

	value = strings.ReplaceAll(value, "%o", names.Options)
	value = strings.ReplaceAll(value, "()", "")
	for strings.Contains(value, "[1]") { value = strings.ReplaceAll(value, "[1]", "") }
	return value
}

// compileKeymap builds a keymap from RMLVO names using the rules file.
func (c *Context) compileKeymap(names *RuleNames) (*Keymap, error) {
	// Apply defaults
	if names.Rules == "" {
		names.Rules = "evdev"
	}
	if names.Model == "" {
		names.Model = "pc105"
	}
	if names.Layout == "" {
		names.Layout = "us"
	}

	// Find and parse rules file
	rulesPath := c.findRulesFile(names.Rules)
	if rulesPath == "" {
		return nil, fmt.Errorf("rules file not found: %s", names.Rules)
	}

	rf, err := parseRulesFile(rulesPath)
	if err != nil {
		return nil, fmt.Errorf("failed to parse rules file: %w", err)
	}

	// Resolve RMLVO to kcCGST
	kccgst := rf.resolve(names)

	// Build keymap string from components
	keymapStr, err := c.buildKeymapString(kccgst)
	if err != nil {
		return nil, fmt.Errorf("failed to build keymap: %w", err)
	}

	// Parse the assembled keymap
	return c.NewKeymapFromString([]byte(keymapStr), KeymapFormatTextV1)
}

// findRulesFile locates a rules file in include paths.
func (c *Context) findRulesFile(name string) string {
	for _, base := range c.IncludePaths() {
		path := filepath.Join(base, "rules", name)
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

// buildKeymapString assembles a complete keymap from kcCGST components.
func (c *Context) buildKeymapString(kccgst *kcCGST) (string, error) {
	var b strings.Builder

	b.WriteString("xkb_keymap {\n")

	// Keycodes section
	keycodes, err := c.loadComponent("keycodes", kccgst.Keycodes)
	if err != nil {
		return "", fmt.Errorf("keycodes: %w", err)
	}
	b.WriteString(keycodes)

	// Types section
	types, err := c.loadComponent("types", kccgst.Types)
	if err != nil {
		return "", fmt.Errorf("types: %w", err)
	}
	b.WriteString(types)

	// Compat section
	compat, err := c.loadComponent("compat", kccgst.Compat)
	if err != nil {
		return "", fmt.Errorf("compat: %w", err)
	}
	b.WriteString(compat)

	// Symbols section
	symbols, err := c.loadComponent("symbols", kccgst.Symbols)
	if err != nil {
		return "", fmt.Errorf("symbols: %w", err)
	}
	b.WriteString(symbols)

	b.WriteString("};\n")

	return b.String(), nil
}

// loadComponent loads and merges XKB component files.
func (c *Context) loadComponent(componentType, spec string) (string, error) {
	if spec == "" {
		return "", fmt.Errorf("no %s component specified", componentType)
	}

	// Parse component spec: file1(section1)+file2(section2)+...
	parts := strings.Split(spec, "+")

	var sections []string
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		content, err := c.loadComponentPart(componentType, part)
		if err != nil {
			return "", err
		}
		sections = append(sections, content)
	}

	// Merge sections into a single component
	return c.mergeComponentSections(componentType, sections), nil
}

// loadComponentPart loads a single component file/section.
func (c *Context) loadComponentPart(componentType, spec string) (string, error) {
	return c.loadComponentPartWithDepth(componentType, spec, 0)
}

// loadComponentPartWithDepth loads a component with include depth tracking.
func (c *Context) loadComponentPartWithDepth(componentType, spec string, depth int) (string, error) {
	if depth > 20 {
		return "", fmt.Errorf("include depth exceeded for %s/%s", componentType, spec)
	}

	fileName, sectionName, groupOffset := spec, "", 0

	// 1. Extract Group Offset (e.g. ":3" from "de:3(e1)" or "de(e1):3")
	for {
		idx := strings.Index(spec, ":")
		if idx == -1 {
			break
		}
		// Scan for continuous digits after colon
		end := idx + 1
		for end < len(spec) && spec[end] >= '0' && spec[end] <= '9' {
			end++
		}
		if end > idx+1 {
			val, _ := strconv.Atoi(spec[idx+1 : end])
			groupOffset = val - 1
			spec = spec[:idx] + spec[end:]
		} else {
			break
		}
	}

	// 2. Extract Section Name (e.g. "(e1)")
	if idx := strings.Index(spec, "("); idx != -1 {
		fileName = spec[:idx]
		sectionName = strings.TrimSuffix(spec[idx+1:], ")")
	} else {
		fileName = spec
	}

	if fileName == "" {
		return "", nil
	}

	filePath := c.findComponentFile(componentType, fileName)
	if filePath == "" {
		return "", fmt.Errorf("component file not found: %s/%s", componentType, fileName)
	}

	data, err := os.ReadFile(filePath)
	if err != nil { return "", err }

	content, err := c.extractSection(string(data), componentType, sectionName)
	if err != nil { return "", err }

	resolved, err := c.resolveIncludes(componentType, content, depth)
	if err != nil { return "", err }

	if componentType == "symbols" && groupOffset > 0 {
		resolved = fmt.Sprintf("group_offset = %d;\n%s\ngroup_offset = 0;\n", groupOffset, resolved)
	}
	return resolved, nil
}

// resolveIncludes replaces include and augment statements with actual content.
func (c *Context) resolveIncludes(componentType, content string, depth int) (string, error) {
	var result strings.Builder
	lines := strings.Split(content, "\n")

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Handle include "file" or augment "file"
		var includeSpec string
		if strings.HasPrefix(trimmed, "include ") {
			includeSpec = strings.TrimPrefix(trimmed, "include ")
		} else if strings.HasPrefix(trimmed, "augment ") {
			includeSpec = strings.TrimPrefix(trimmed, "augment ")
		}

		if includeSpec != "" {
			includeSpec = strings.Trim(includeSpec, "\"")

			// Load the included content
			included, err := c.loadComponentPartWithDepth(componentType, includeSpec, depth+1)
			if err != nil {
				// Log warning but continue - some includes might be optional
				c.log(0, "warning: failed to resolve include", "spec", includeSpec, "error", err)
				continue
			}
			result.WriteString(included)
			result.WriteString("\n")
		} else {
			result.WriteString(line)
			result.WriteString("\n")
		}
	}

	return result.String(), nil
}

// findComponentFile locates a component file in include paths.
func (c *Context) findComponentFile(componentType, name string) string {
	for _, base := range c.IncludePaths() {
		path := filepath.Join(base, componentType, name)
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

// extractSection extracts a named section from component file content.
func (c *Context) extractSection(content, componentType, sectionName string) (string, error) {
	// Map component type to XKB section keyword variants
	// The keyword can be xkb_keycodes, xkb_types, xkb_compat, xkb_compatibility, xkb_symbols
	keywords := []string{"xkb_" + componentType}
	if componentType == "compat" {
		keywords = append(keywords, "xkb_compatibility")
	}

	// Lines that might have section declarations can have modifiers before keyword:
	// "default partial xkb_compatibility "name" { ... }"
	// We need to find the line containing the keyword and section name

	var idx int = -1
	for _, keyword := range keywords {
		if sectionName != "" {
			// Look for exact section name match
			pattern := keyword + ` "` + sectionName + `"`
			idx = strings.Index(content, pattern)
			if idx != -1 {
				break
			}
		} else {
			// Look for default section - find keyword followed by "{" (possibly with name)
			// or just the keyword at all (take first)
			idx = strings.Index(content, keyword)
			if idx != -1 {
				break
			}
		}
	}

	if idx == -1 {
		return "", fmt.Errorf("section not found: xkb_%s %s", componentType, sectionName)
	}

	// Find the section content (between { and matching })
	start := strings.Index(content[idx:], "{")
	if start == -1 {
		return "", fmt.Errorf("malformed section: no opening brace")
	}
	start += idx

	// Find matching closing brace
	depth := 1
	end := start + 1
	for end < len(content) && depth > 0 {
		switch content[end] {
		case '{':
			depth++
		case '}':
			depth--
		}
		end++
	}

	if depth != 0 {
		return "", fmt.Errorf("malformed section: unmatched braces")
	}

	// Return the section content (without the outer braces)
	return content[start+1 : end-1], nil
}

// mergeComponentSections merges multiple section contents into one.
func (c *Context) mergeComponentSections(componentType string, sections []string) string {
	var b strings.Builder

	keyword := "xkb_" + componentType
	b.WriteString(keyword + " {\n")

	for _, section := range sections {
		b.WriteString(section)
		b.WriteString("\n")
	}

	b.WriteString("};\n")

	return b.String()
}
