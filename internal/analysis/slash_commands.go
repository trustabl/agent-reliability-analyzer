package analysis

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/trustabl/trustabl/internal/models"
)

type slashCommandFrontmatter struct {
	Description            string       `yaml:"description"`
	AllowedTools           stringOrList `yaml:"allowed-tools"`
	Model                  string       `yaml:"model"`
	ArgumentHint           string       `yaml:"argument-hint"`
	DisableModelInvocation lenientBool  `yaml:"disable-model-invocation"`
}

// lenientBool unmarshals a YAML scalar that reads as true into true and any
// other scalar into false, instead of failing. A strict bool field makes
// yaml.v3 reject a quoted "true" or the number 1, and the caller then drops the
// whole frontmatter, so a command's allowed-tools would be lost along with it.
// Spellings read as true: true, yes, on, y and 1 in any letter case, quoted or
// not. This is a reading of intent, not a statement of how Claude Code parses
// the key: neither its docs nor its source were found to say what a string
// value does. Reading a string as true suppresses a finding that depends on the
// command being model-invocable (CCMD-002), the same direction an unquoted
// `yes` already takes.
type lenientBool bool

func (b *lenientBool) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind != yaml.ScalarNode {
		*b = false
		return nil
	}
	switch strings.ToLower(strings.TrimSpace(value.Value)) {
	case "true", "yes", "on", "y", "1":
		*b = true
	default:
		*b = false
	}
	return nil
}

// DiscoverSlashCommands parses every ComponentSlashCommand (.claude/commands/*.md)
// in the manifest. The command name is the file basename without extension. A
// command without frontmatter is still emitted (its body is the prompt); only
// the name and location are populated in that case.
func DiscoverSlashCommands(manifest models.ScanManifest) []models.SlashCommandDef {
	var out []models.SlashCommandDef
	for _, c := range manifest.Components {
		if c.Kind != models.ComponentSlashCommand {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(manifest.RepoRoot, c.Path))
		if err != nil {
			continue
		}
		name := strings.TrimSuffix(filepath.Base(c.Path), ".md")
		def := models.SlashCommandDef{
			Name:     name,
			Location: models.Location{FilePath: c.Path, Line: 1, EndLine: bytes.Count(raw, []byte{'\n'}) + 1},
		}
		if fm, startLine, endLine, ok := extractFrontmatter(raw); ok {
			var parsed slashCommandFrontmatter
			if err := yaml.Unmarshal(fm, &parsed); err == nil {
				tokens := splitToolsTokens([]string(parsed.AllowedTools))
				def.Description = parsed.Description
				def.AllowedTools = tokens
				def.ToolGrants = parseToolGrants(tokens)
				def.Model = parsed.Model
				def.ArgumentHint = parsed.ArgumentHint
				def.DisableModelInvocation = bool(parsed.DisableModelInvocation)
				def.Line = startLine
				def.EndLine = endLine
			}
		}
		out = append(out, def)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].FilePath < out[j].FilePath })
	return out
}
