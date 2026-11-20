package config

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"unicode"
)

// KnownPermissions lists all supported Gopkin unified permission keys.
var KnownPermissions = map[string]bool{
	"location":      true,
	"camera":        true,
	"bluetooth":     true,
	"notifications": true,
	"storage":       true,
}

// Manifest represents a Gopkin project's gopkin.yaml configuration.
type Manifest struct {
	Name        string
	ID          string
	Version     string
	Entry       string
	Permissions map[string]string
}

// DefaultManifest returns a clean default Manifest for a new Gopkin application.
func DefaultManifest(appName string) Manifest {
	name := strings.TrimSpace(appName)
	if name == "" {
		name = "MyApp"
	}
	return Manifest{
		Name:        name,
		ID:          "com.example." + sanitizeAppIDSegment(name),
		Version:     "0.1.0",
		Entry:       "app/main.gpk",
		Permissions: map[string]string{},
	}
}

// LoadManifest reads and parses a gopkin.yaml file from path.
func LoadManifest(path string) (Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, fmt.Errorf("config: read manifest %q: %w", path, err)
	}
	return ParseManifest(string(data))
}

// ParseManifest parses a minimal Pure-Go YAML manifest without external dependencies.
func ParseManifest(content string) (Manifest, error) {
	m := Manifest{
		Version:     "0.1.0",
		Entry:       "app/main.gpk",
		Permissions: make(map[string]string),
	}

	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	inPermissions := false

	for lineNum, raw := range lines {
		cleaned := stripYAMLComment(raw)
		if strings.TrimSpace(cleaned) == "" {
			continue
		}

		indent := countLeadingSpaces(cleaned)
		trimmed := strings.TrimSpace(cleaned)

		if indent == 0 {
			inPermissions = false
			colonIdx := strings.IndexByte(trimmed, ':')
			if colonIdx <= 0 {
				return m, fmt.Errorf("config: line %d: expected key: value, got %q", lineNum+1, trimmed)
			}
			key := strings.ToLower(strings.TrimSpace(trimmed[:colonIdx]))
			val := unquoteYAML(strings.TrimSpace(trimmed[colonIdx+1:]))

			switch key {
			case "name":
				m.Name = val
			case "id":
				m.ID = val
			case "version":
				if val != "" {
					m.Version = val
				}
			case "entry":
				if val != "" {
					m.Entry = val
				}
			case "permissions":
				if val == "" {
					inPermissions = true
				} else if val == "{}" || val == "[]" {
					inPermissions = false
				} else {
					return m, fmt.Errorf("config: line %d: permissions must be a YAML map or list", lineNum+1)
				}
			}
			continue
		}

		if inPermissions {
			if strings.HasPrefix(trimmed, "-") {
				// List item form: "- location" or "- camera"
				perm := strings.ToLower(unquoteYAML(strings.TrimSpace(strings.TrimPrefix(trimmed, "-"))))
				if perm == "" {
					continue
				}
				m.Permissions[perm] = ""
				continue
			}

			// Map entry form: "location: \"Used to find nearby services\"" or "bluetooth:"
			colonIdx := strings.IndexByte(trimmed, ':')
			if colonIdx <= 0 {
				return m, fmt.Errorf("config: line %d: invalid permission entry %q", lineNum+1, trimmed)
			}
			perm := strings.ToLower(strings.TrimSpace(trimmed[:colonIdx]))
			reason := unquoteYAML(strings.TrimSpace(trimmed[colonIdx+1:]))
			m.Permissions[perm] = reason
		}
	}

	// Fill default descriptions for permissions declared without a custom reason.
	for perm, reason := range m.Permissions {
		if strings.TrimSpace(reason) == "" {
			m.Permissions[perm] = defaultPermissionReason(perm, m.Name)
		}
	}

	if err := m.Validate(); err != nil {
		return m, err
	}
	return m, nil
}

// Validate checks that the manifest has valid required fields and known permissions.
func (m Manifest) Validate() error {
	if strings.TrimSpace(m.Name) == "" {
		return errors.New("config: manifest 'name' is required")
	}
	if strings.TrimSpace(m.ID) == "" {
		return errors.New("config: manifest 'id' is required (e.g. com.example.myapp)")
	}
	if !isValidBundleID(m.ID) {
		return fmt.Errorf("config: invalid manifest 'id' %q (expected reverse-DNS format like com.example.myapp)", m.ID)
	}
	if strings.TrimSpace(m.Entry) == "" {
		return errors.New("config: manifest 'entry' cannot be empty")
	}
	for perm := range m.Permissions {
		if !KnownPermissions[perm] {
			return fmt.Errorf("config: unknown permission %q (supported: location, camera, bluetooth, notifications, storage)", perm)
		}
	}
	return nil
}

// MarshalManifest serializes m into a clean, readable gopkin.yaml document.
func MarshalManifest(m Manifest) string {
	var b strings.Builder
	b.WriteString("name: " + m.Name + "\n")
	b.WriteString("id: " + m.ID + "\n")
	version := m.Version
	if version == "" {
		version = "0.1.0"
	}
	b.WriteString("version: " + version + "\n")
	entry := m.Entry
	if entry == "" {
		entry = "app/main.gpk"
	}
	b.WriteString("entry: " + entry + "\n")

	if len(m.Permissions) > 0 {
		b.WriteString("permissions:\n")
		keys := make([]string, 0, len(m.Permissions))
		for k := range m.Permissions {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			reason := strings.ReplaceAll(m.Permissions[k], `"`, `\"`)
			b.WriteString(fmt.Sprintf("  %s: %q\n", k, reason))
		}
	} else {
		b.WriteString("# permissions:\n")
		b.WriteString("#   location: \"Used to find nearby services\"\n")
		b.WriteString("#   camera: \"Used to scan QR codes\"\n")
	}
	return b.String()
}

func defaultPermissionReason(perm, appName string) string {
	if strings.TrimSpace(appName) == "" {
		appName = "This app"
	}
	switch perm {
	case "location":
		return appName + " uses your location to provide location-aware features."
	case "camera":
		return appName + " uses the camera to capture photos and scan codes."
	case "bluetooth":
		return appName + " uses Bluetooth to connect to nearby devices."
	case "notifications":
		return appName + " sends notifications to keep you updated."
	case "storage":
		return appName + " accesses device storage to save and load files."
	default:
		return appName + " requires " + perm + " access."
	}
}

func sanitizeAppIDSegment(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "app"
	}
	s := b.String()
	if s[0] >= '0' && s[0] <= '9' {
		return "app" + s
	}
	return s
}

func isValidBundleID(id string) bool {
	parts := strings.Split(id, ".")
	if len(parts) < 2 {
		return false
	}
	for _, seg := range parts {
		if seg == "" {
			return false
		}
		for _, r := range seg {
			if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' && r != '-' {
				return false
			}
		}
	}
	return true
}

func countLeadingSpaces(s string) int {
	n := 0
	for _, r := range s {
		if r == ' ' {
			n++
		} else if r == '\t' {
			n += 2
		} else {
			break
		}
	}
	return n
}

func stripYAMLComment(line string) string {
	inSingle := false
	inDouble := false
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle && (i == 0 || line[i-1] != '\\') {
				inDouble = !inDouble
			}
		case '#':
			if !inSingle && !inDouble {
				return line[:i]
			}
		}
	}
	return line
}

func unquoteYAML(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			inner := s[1 : len(s)-1]
			if s[0] == '"' {
				inner = strings.ReplaceAll(inner, `\"`, `"`)
				inner = strings.ReplaceAll(inner, `\\`, `\`)
			}
			return inner
		}
	}
	return s
}
