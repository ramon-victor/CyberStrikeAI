package config

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type nucleiWrapperHarness struct {
	wrapperPath  string
	realPath     string
	logPath      string
	templatesDir string
	stampPath    string
}

func nucleiWrapperScriptPath(t *testing.T) string {
	t.Helper()

	path := filepath.Clean(filepath.Join("..", "..", "scripts", "docker", "nuclei-wrapper.sh"))
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("nuclei wrapper script not found: %v", err)
	}
	return path
}

func newNucleiWrapperHarness(t *testing.T) nucleiWrapperHarness {
	t.Helper()

	tmpDir := t.TempDir()
	realPath := filepath.Join(tmpDir, "nuclei.real")
	fakeReal := []byte(`#!/usr/bin/env bash
set -u
{
    echo BEGIN
    for arg in "$@"; do
        echo "${arg}"
    done
    echo END
} >>"${NUCLEI_FAKE_LOG}"
if [[ "${NUCLEI_FAKE_FAIL_UPDATE:-}" == "true" ]]; then
    for arg in "$@"; do
        case "${arg}" in
            -update-templates|--update-templates) exit 23 ;;
        esac
    done
fi
exit 0
`)
	if err := os.WriteFile(realPath, fakeReal, 0755); err != nil {
		t.Fatalf("write fake nuclei.real: %v", err)
	}

	templatesDir := filepath.Join(tmpDir, "nuclei-templates")
	return nucleiWrapperHarness{
		wrapperPath:  nucleiWrapperScriptPath(t),
		realPath:     realPath,
		logPath:      filepath.Join(tmpDir, "nuclei.log"),
		templatesDir: templatesDir,
		stampPath:    filepath.Join(templatesDir, ".cyberstrike-last-template-update"),
	}
}

func (h nucleiWrapperHarness) run(t *testing.T, args []string, extraEnv ...string) ([][]string, string) {
	t.Helper()

	cmdArgs := append([]string{h.wrapperPath}, args...)
	cmd := exec.Command("bash", cmdArgs...)
	cmd.Env = append(os.Environ(),
		"CYBERSTRIKE_NUCLEI_AUTO_UPDATE=true",
		"CYBERSTRIKE_NUCLEI_REAL="+h.realPath,
		"CYBERSTRIKE_NUCLEI_UPDATE_INTERVAL_SECONDS=86400",
		"CYBERSTRIKE_NUCLEI_UPDATE_STAMP="+h.stampPath,
		"NUCLEI_FAKE_LOG="+h.logPath,
		"NUCLEI_TEMPLATES_PATH="+h.templatesDir,
	)
	cmd.Env = append(cmd.Env, extraEnv...)

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run nuclei wrapper: %v\n%s", err, out)
	}
	return readNucleiFakeInvocations(t, h.logPath), string(out)
}

func readNucleiFakeInvocations(t *testing.T, logPath string) [][]string {
	t.Helper()

	content, err := os.ReadFile(logPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatalf("read fake nuclei log: %v", err)
	}

	var invocations [][]string
	var current []string
	for _, line := range strings.Split(strings.TrimSuffix(string(content), "\n"), "\n") {
		switch line {
		case "BEGIN":
			if current != nil {
				t.Fatalf("nested invocation in fake nuclei log: %q", content)
			}
			current = []string{}
		case "END":
			if current == nil {
				t.Fatalf("unmatched invocation end in fake nuclei log: %q", content)
			}
			invocations = append(invocations, current)
			current = nil
		default:
			if current == nil {
				t.Fatalf("argument outside invocation in fake nuclei log: %q", content)
			}
			current = append(current, line)
		}
	}
	if current != nil {
		t.Fatalf("unterminated invocation in fake nuclei log: %q", content)
	}
	return invocations
}

func TestNucleiWrapperRefreshesTemplatesAndSuppressesUpdateCheck(t *testing.T) {
	h := newNucleiWrapperHarness(t)

	invocations, output := h.run(t, []string{"-u", "https://example.test"})
	if output != "" {
		t.Fatalf("wrapper should not write output on successful refresh, got %q", output)
	}

	want := [][]string{
		{"-update-templates", "-ud", h.templatesDir},
		{"-duc", "-u", "https://example.test"},
	}
	if !reflect.DeepEqual(invocations, want) {
		t.Fatalf("unexpected nuclei invocations\nwant: %#v\ngot:  %#v", want, invocations)
	}
	if _, err := os.Stat(h.stampPath); err != nil {
		t.Fatalf("wrapper should write update stamp after successful refresh: %v", err)
	}
}

func TestNucleiWrapperDoesNotDuplicateDisableUpdateCheck(t *testing.T) {
	h := newNucleiWrapperHarness(t)

	invocations, _ := h.run(t, []string{"-duc", "-u", "https://example.test"})
	want := [][]string{
		{"-update-templates", "-ud", h.templatesDir},
		{"-duc", "-u", "https://example.test"},
	}
	if !reflect.DeepEqual(invocations, want) {
		t.Fatalf("unexpected nuclei invocations\nwant: %#v\ngot:  %#v", want, invocations)
	}
}

func TestNucleiWrapperPassesManualUpdateCommandsThrough(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
	}{
		{name: "long", args: []string{"-update-templates", "-ud", "/custom/templates"}},
		{name: "short", args: []string{"-ut"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newNucleiWrapperHarness(t)

			invocations, _ := h.run(t, tc.args)
			want := [][]string{tc.args}
			if !reflect.DeepEqual(invocations, want) {
				t.Fatalf("manual update should pass through without auto-update or -duc\nwant: %#v\ngot:  %#v", want, invocations)
			}
			if _, err := os.Stat(h.stampPath); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("manual update pass-through should not write wrapper stamp, stat err: %v", err)
			}
		})
	}
}

func TestNucleiWrapperSkipsRefreshWhenAutoUpdateDisabled(t *testing.T) {
	h := newNucleiWrapperHarness(t)

	invocations, _ := h.run(t, []string{"-u", "https://example.test"}, "CYBERSTRIKE_NUCLEI_AUTO_UPDATE=false")
	want := [][]string{{"-duc", "-u", "https://example.test"}}
	if !reflect.DeepEqual(invocations, want) {
		t.Fatalf("auto-update disabled should skip refresh but still suppress checks\nwant: %#v\ngot:  %#v", want, invocations)
	}
	if _, err := os.Stat(h.stampPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("auto-update disabled should not write wrapper stamp, stat err: %v", err)
	}
}

func TestNucleiWrapperContinuesWhenRefreshFails(t *testing.T) {
	h := newNucleiWrapperHarness(t)

	invocations, output := h.run(t, []string{"-u", "https://example.test"}, "NUCLEI_FAKE_FAIL_UPDATE=true")
	want := [][]string{
		{"-update-templates", "-ud", h.templatesDir},
		{"-duc", "-u", "https://example.test"},
	}
	if !reflect.DeepEqual(invocations, want) {
		t.Fatalf("failed refresh should still run scan with update checks suppressed\nwant: %#v\ngot:  %#v", want, invocations)
	}
	if !strings.Contains(output, "failed to refresh nuclei templates") {
		t.Fatalf("failed refresh should emit a clear warning, got %q", output)
	}
	if _, err := os.Stat(h.stampPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed refresh should not write wrapper stamp, stat err: %v", err)
	}
}
