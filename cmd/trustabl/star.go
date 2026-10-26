package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/mattn/go-isatty"

	"github.com/trustabl/trustabl/internal/telemetry"
)

const starPromptLine = "⭐ Found this useful? Star Agent Reliability Analyzer: https://github.com/trustabl/agent-reliability-analyzer"

// starPromptMarkerPath is a marker file next to telemetry.json. Its presence
// means the star line was already shown. Empty when the home dir is unknown.
func starPromptMarkerPath() string {
	p, err := telemetry.DefaultConfigPath()
	if err != nil {
		return ""
	}
	return filepath.Join(filepath.Dir(p), "star-prompt-shown")
}

// starPromptInteractive is true only for a person at a terminal: stdout and
// stderr are both TTYs and no CI system is detected.
func starPromptInteractive() bool {
	return isatty.IsTerminal(os.Stdout.Fd()) && isatty.IsTerminal(os.Stderr.Fd()) &&
		os.Getenv("CI") == "" && telemetry.DetectCIProvider() == ""
}

// maybeStarPrompt prints the star line to w once, after the first successful
// interactive scan with human output. The marker is written before printing,
// so a run that cannot save it never prints (no nagging on every scan).
func maybeStarPrompt(w io.Writer, format string, interactive bool, markerPath string) {
	if format != "human" || !interactive || markerPath == "" {
		return
	}
	if _, err := os.Stat(markerPath); err == nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(markerPath), 0o755); err != nil {
		return
	}
	if err := os.WriteFile(markerPath, nil, 0o644); err != nil {
		return
	}
	fmt.Fprintln(w, starPromptLine)
}
