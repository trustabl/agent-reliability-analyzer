package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/trustabl/trustabl/internal/forge"
	"github.com/trustabl/trustabl/internal/models"
	"github.com/trustabl/trustabl/internal/rules"
	"github.com/trustabl/trustabl/internal/rulesource"
	"github.com/trustabl/trustabl/internal/telemetry"
)

func newForgeCommand(tel *telemetry.Client) *cobra.Command {
	var policyFlags, langFlags []string
	var output, rulesRef string

	cmd := &cobra.Command{
		Use:   "forge [target]",
		Short: "Generate a pre-coding reliability skill for detected SDKs",
		Long: `Generate a combined pre-coding SKILL.md from the detection rules for the SDKs
found in a target repository.

[target] is an optional local path (directory or file). Defaults to the current
working directory. Trustabl scans the target's dependency manifests
(pyproject.toml, package.json, go.mod, etc.) to determine which SDK policy
packs to include.

Use --policy to add categories on top of auto-detected ones — useful when a
new SDK is being introduced to a repo before its first dependency declaration.

Use --lang to add a language for the Runtime Tracing section on top of the
ones detected from the dependency manifests — useful for a brand-new repo that
has no manifest yet. Accepted values: python, typescript, go.

The output is a SKILL.md written to stdout (or --output) that opens with a
"How to Apply These Constraints" section describing the apply loop. After it
comes a "Runtime Tracing" section for the selected languages, then one section
per detected SDK, with rules ordered by severity.`,
		Example: `  # Auto-detect SDKs from current directory
  trustabl forge

  # Auto-detect from a specific repo path
  trustabl forge /path/to/repo

  # Always include openai_sdk on top of whatever is detected
  trustabl forge --policy openai_sdk

  # Multiple explicit additions
  trustabl forge --policy openai_sdk,mcp --output pre-coding.md

  # Generate only for claude_skill (backward-compatible)
  trustabl forge --policy claude_skill

  # New Go repo with no go.mod yet
  trustabl forge --policy google_adk --lang go`,
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			target := "."
			if len(args) > 0 {
				target = args[0]
			}

			// parse --policy as comma-separated DetectorCategory values
			var explicitCats []models.DetectorCategory
			for _, flag := range policyFlags {
				for _, part := range strings.Split(flag, ",") {
					part = strings.TrimSpace(part)
					if part == "" {
						continue
					}
					if !models.ValidCategory(models.DetectorCategory(part)) {
						fmt.Fprintf(cmd.ErrOrStderr(),
							"trustabl forge: unknown policy category %q; accepted values: %v\n",
							part, models.AllCategories)
						return exitCodeError{code: 1}
					}
					explicitCats = append(explicitCats, models.DetectorCategory(part))
				}
			}

			explicitLangs, bad := parseForgeLangs(langFlags)
			if bad != "" {
				fmt.Fprintf(cmd.ErrOrStderr(),
					"trustabl forge: unknown language %q; accepted values: python, typescript, go\n", bad)
				return exitCodeError{code: 1}
			}

			if tel != nil {
				tel.Track("command.run", map[string]any{
					"command":        "forge",
					"explicit_count": len(explicitCats),
				})
			}

			return runForge(cmd, target, explicitCats, explicitLangs, output, rulesRef)
		},
	}

	cmd.Flags().StringSliceVar(&policyFlags, "policy", nil,
		"policy categories to include (e.g. openai_sdk,mcp); additive on top of auto-detected SDKs")
	cmd.Flags().StringSliceVar(&langFlags, "lang", nil,
		"languages for the Runtime Tracing section (python,typescript,go); additive on top of detected languages")
	cmd.Flags().StringVarP(&output, "output", "o", "",
		"write the generated SKILL.md to this path (default: stdout)")
	cmd.Flags().StringVar(&rulesRef, "rules-ref", "",
		"pin the agent-reliability-rules branch or tag (default: latest cached)")

	cmd.AddCommand(newForgeCheckCommand())

	return cmd
}

// parseForgeLangs parses --lang values. It returns the first unrecognized
// value as bad, and the languages otherwise.
func parseForgeLangs(flags []string) (langs []models.Language, bad string) {
	for _, flag := range flags {
		for _, part := range strings.Split(flag, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			lang, ok := forge.ParseTracingLanguage(part)
			if !ok {
				return nil, part
			}
			langs = append(langs, lang)
		}
	}
	return langs, ""
}

func runForge(cmd *cobra.Command, target string, explicit []models.DetectorCategory, explicitLangs []models.Language, output, rulesRef string) error {
	ctx := cmd.Context()

	// Step 1: auto-detect SDKs, and the language each is declared in, from dep manifests
	det, err := forge.Detect(ctx, target)
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "trustabl forge: detect SDKs: %v\n", err)
		return exitCodeError{code: 1}
	}

	// Step 2: merge with explicit --policy values
	categories := forge.MergeCategories(det.Categories, explicit)
	if len(categories) == 0 {
		fmt.Fprintf(cmd.ErrOrStderr(),
			"trustabl forge: no SDKs detected in %q and no --policy specified\n"+
				"  run 'trustabl forge --policy <category>' to specify one explicitly\n"+
				"  accepted categories: %v\n",
			target, models.AllCategories)
		return exitCodeError{code: 1}
	}

	tracing := forge.SelectTracing(det, categories, explicit, explicitLangs)
	if len(tracing.Languages) == 0 {
		// stderr only: stdout carries the generated skill.
		fmt.Fprintf(cmd.ErrOrStderr(),
			"trustabl forge: no supported language detected (python, typescript, go); "+
				"the Runtime Tracing section is omitted — pass --lang to add one\n")
	}

	// Step 3: resolve rules
	res, err := rulesource.Resolve(
		rulesource.Config{Ref: rulesRef},
		rules.SupportedSchemaVersion,
	)
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "trustabl forge: rules: %v\n", err)
		return exitCodeError{code: 2}
	}

	policies, _, err := rules.LoadLenient(res.FS)
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "trustabl forge: load rules: %v\n", err)
		return exitCodeError{code: 2}
	}

	// Step 4: build stamp (passive watermark)
	stamp := buildForgeStamp(time.Now().Format("2006-01-02"), res.SHA, res.SchemaVersion, categories)

	// Step 5: generate and emit
	content := forge.GenerateCombined(categories, policies, stamp, tracing)
	if output == "" {
		fmt.Fprint(cmd.OutOrStdout(), content)
		return nil
	}
	return os.WriteFile(output, []byte(content), 0o644)
}

// buildForgeStamp assembles the passive watermark stamped into a generated
// SKILL.md. It is a pure function so runForge's stamp assembly — including
// the always-current forge.TemplateVersion — can be tested without a rules
// resolution or filesystem write.
func buildForgeStamp(date, sha string, schema int, cats []models.DetectorCategory) forge.PolicyStamp {
	return forge.PolicyStamp{
		Date:          date,
		RulesSHA:      sha,
		SchemaVersion: schema,
		Categories:    cats,
		Template:      forge.TemplateVersion,
	}
}
