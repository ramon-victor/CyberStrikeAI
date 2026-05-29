package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type dockerToolYAML struct {
	Name    string `yaml:"name"`
	Command string `yaml:"command"`
	Enabled bool   `yaml:"enabled"`
}

func repoFilePath(t *testing.T, elements ...string) string {
	t.Helper()

	parts := append([]string{"..", ".."}, elements...)
	path := filepath.Clean(filepath.Join(parts...))
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("repo file %q not found: %v", path, err)
	}
	return path
}

func dockerfileLineContainsAll(dockerfile string, needles ...string) bool {
	for _, line := range strings.Split(dockerfile, "\n") {
		hasAllNeedles := true
		for _, needle := range needles {
			if !strings.Contains(line, needle) {
				hasAllNeedles = false
				break
			}
		}
		if hasAllNeedles {
			return true
		}
	}
	return false
}

func TestDockerfileInstallsEnabledX8Tool(t *testing.T) {
	dockerfileBytes, err := os.ReadFile(repoFilePath(t, "Dockerfile"))
	if err != nil {
		t.Fatalf("read Dockerfile: %v", err)
	}
	dockerfile := string(dockerfileBytes)

	x8ToolBytes, err := os.ReadFile(repoFilePath(t, "tools", "x8.yaml"))
	if err != nil {
		t.Fatalf("read x8 tool definition: %v", err)
	}

	var tool dockerToolYAML
	if err := yaml.Unmarshal(x8ToolBytes, &tool); err != nil {
		t.Fatalf("parse x8 tool definition: %v", err)
	}
	if tool.Name != "x8" {
		t.Fatalf("x8 tool definition name = %q, want x8", tool.Name)
	}
	if !tool.Enabled {
		t.Fatal("x8 tool definition must remain enabled")
	}
	if tool.Command != "x8" {
		t.Fatalf("x8 tool command = %q, want x8", tool.Command)
	}

	if !strings.Contains(dockerfile, "cargo install x8") {
		t.Fatal("Dockerfile rust-tools-builder stage must install x8 with cargo")
	}

	if !dockerfileLineContainsAll(dockerfile, "cp ", "/usr/local/cargo/bin/x8", "/out/bin") {
		t.Fatal("Dockerfile rust-tools-builder stage must copy x8 into /out/bin")
	}

	if !dockerfileLineContainsAll(dockerfile, "RUN for command in ", " x8 ", "; do \\") {
		t.Fatal("Dockerfile runtime required-command verification must include x8")
	}
}
