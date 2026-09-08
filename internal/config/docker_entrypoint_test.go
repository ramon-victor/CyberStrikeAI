package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func dockerEntrypointScriptPath(t *testing.T) string {
	t.Helper()

	path := filepath.Clean(filepath.Join("..", "..", "scripts", "docker", "docker-entrypoint.sh"))
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("docker entrypoint script not found: %v", err)
	}
	return path
}

func TestDockerEntrypointInitializesRuntimeConfigFromTemplate(t *testing.T) {
	tmpDir := t.TempDir()
	templatePath := filepath.Join(tmpDir, "config.example.yaml")
	runtimeDir := filepath.Join(tmpDir, "runtime-config")
	runtimeConfigPath := filepath.Join(runtimeDir, "config.yaml")
	templateContent := []byte("auth:\n  password: \n")

	if err := os.WriteFile(templatePath, templateContent, 0644); err != nil {
		t.Fatalf("write template config: %v", err)
	}

	cmd := exec.Command("bash", dockerEntrypointScriptPath(t), "true")
	cmd.Env = append(os.Environ(),
		"CYBERSTRIKE_APP_VERSION=v1.7.18",
		"CYBERSTRIKE_TEMPLATE_CONFIG="+templatePath,
		"CYBERSTRIKE_RUNTIME_CONFIG_DIR="+runtimeDir,
		"CYBERSTRIKE_RUNTIME_CONFIG_PATH="+runtimeConfigPath,
	)

	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("run docker entrypoint: %v\n%s", err, out)
	}

	got, err := os.ReadFile(runtimeConfigPath)
	if err != nil {
		t.Fatalf("read runtime config: %v", err)
	}
	want := "version: \"v1.7.18\"\n" + string(templateContent)
	if string(got) != want {
		t.Fatalf("runtime config mismatch\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestDockerEntrypointPreservesExistingRuntimeConfig(t *testing.T) {
	tmpDir := t.TempDir()
	templatePath := filepath.Join(tmpDir, "config.example.yaml")
	runtimeDir := filepath.Join(tmpDir, "runtime-config")
	runtimeConfigPath := filepath.Join(runtimeDir, "config.yaml")
	templateContent := []byte("auth:\n  password: \n")
	existingContent := []byte("auth:\n  password: existing-secret\n")

	if err := os.WriteFile(templatePath, templateContent, 0644); err != nil {
		t.Fatalf("write template config: %v", err)
	}
	if err := os.MkdirAll(runtimeDir, 0755); err != nil {
		t.Fatalf("mkdir runtime dir: %v", err)
	}
	if err := os.WriteFile(runtimeConfigPath, existingContent, 0644); err != nil {
		t.Fatalf("write runtime config: %v", err)
	}

	cmd := exec.Command("bash", dockerEntrypointScriptPath(t), "true")
	cmd.Env = append(os.Environ(),
		"CYBERSTRIKE_APP_VERSION=v1.7.18",
		"CYBERSTRIKE_TEMPLATE_CONFIG="+templatePath,
		"CYBERSTRIKE_RUNTIME_CONFIG_DIR="+runtimeDir,
		"CYBERSTRIKE_RUNTIME_CONFIG_PATH="+runtimeConfigPath,
	)

	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("run docker entrypoint: %v\n%s", err, out)
	}

	got, err := os.ReadFile(runtimeConfigPath)
	if err != nil {
		t.Fatalf("read runtime config: %v", err)
	}
	want := "version: \"v1.7.18\"\n" + string(existingContent)
	if string(got) != want {
		t.Fatalf("runtime config should preserve settings\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestDockerEntrypointUpdatesOnlyTopLevelVersion(t *testing.T) {
	tmpDir := t.TempDir()
	templatePath := filepath.Join(tmpDir, "config.example.yaml")
	runtimeDir := filepath.Join(tmpDir, "runtime-config")
	runtimeConfigPath := filepath.Join(runtimeDir, "config.yaml")
	content := []byte("version: \"v1.0.0\"\nserver:\n  version: nested\n  port: 7022\n")

	if err := os.WriteFile(templatePath, content, 0644); err != nil {
		t.Fatalf("write template config: %v", err)
	}
	if err := os.MkdirAll(runtimeDir, 0755); err != nil {
		t.Fatalf("mkdir runtime dir: %v", err)
	}
	if err := os.WriteFile(runtimeConfigPath, content, 0644); err != nil {
		t.Fatalf("write runtime config: %v", err)
	}

	cmd := exec.Command("bash", dockerEntrypointScriptPath(t), "true")
	cmd.Env = append(os.Environ(),
		"CYBERSTRIKE_APP_VERSION=v1.7.18",
		"CYBERSTRIKE_TEMPLATE_CONFIG="+templatePath,
		"CYBERSTRIKE_RUNTIME_CONFIG_DIR="+runtimeDir,
		"CYBERSTRIKE_RUNTIME_CONFIG_PATH="+runtimeConfigPath,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("run docker entrypoint: %v\n%s", err, out)
	}

	got, err := os.ReadFile(runtimeConfigPath)
	if err != nil {
		t.Fatalf("read runtime config: %v", err)
	}
	want := "version: \"v1.7.18\"\nserver:\n  version: nested\n  port: 7022\n"
	if string(got) != want {
		t.Fatalf("entrypoint changed more than top-level version\nwant:\n%s\ngot:\n%s", want, got)
	}
}
