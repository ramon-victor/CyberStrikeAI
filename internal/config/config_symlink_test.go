package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadResolvesRelativeDirsFromSymlinkTarget(t *testing.T) {
	tmpDir := t.TempDir()
	runtimeDir := filepath.Join(tmpDir, "runtime-config")
	rolesDir := filepath.Join(tmpDir, "roles")
	if err := os.MkdirAll(runtimeDir, 0755); err != nil {
		t.Fatalf("mkdir runtime dir: %v", err)
	}
	if err := os.MkdirAll(rolesDir, 0755); err != nil {
		t.Fatalf("mkdir roles dir: %v", err)
	}

	configPath := filepath.Join(runtimeDir, "config.yaml")
	configContent := []byte("auth:\n  password: test-password\nroles_dir: ../roles\n")
	if err := os.WriteFile(configPath, configContent, 0644); err != nil {
		t.Fatalf("write runtime config: %v", err)
	}

	roleContent := []byte("name: Analyst\nname_en: Analyst\ndescription: Analysis role\ndescription_en: Analysis role\nuser_prompt: analyze\nicon: 🧠\nenabled: true\n")
	if err := os.WriteFile(filepath.Join(rolesDir, "analyst.yaml"), roleContent, 0644); err != nil {
		t.Fatalf("write role config: %v", err)
	}

	linkPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.Symlink(configPath, linkPath); err != nil {
		t.Fatalf("create config symlink: %v", err)
	}

	cfg, err := Load(linkPath)
	if err != nil {
		t.Fatalf("load config through symlink: %v", err)
	}
	if _, ok := cfg.Roles["Analyst"]; !ok {
		t.Fatalf("expected role loaded from directory relative to symlink target, got %#v", cfg.Roles)
	}
}
