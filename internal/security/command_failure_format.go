package security

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// FormatCommandFailureResult matches the exec tool ToolResult text (without ToolErrorPrefix).
func FormatCommandFailureResult(exitCode int, output string) string {
	output = strings.TrimSpace(output)
	errMsg := fmt.Sprintf("exit status %d", exitCode)
	if output == "" {
		return fmt.Sprintf("Command execution failed: %s", errMsg)
	}
	if strings.HasPrefix(output, "Command execution failed:") {
		return output
	}
	return fmt.Sprintf("Command execution failed: %s\nOutput: %s", errMsg, output)
}

// FormatCommandFailureFromErr generates uniform failure text from exec/execute returned error (IsError body).
func FormatCommandFailureFromErr(err error, output string) string {
	if err == nil {
		return strings.TrimSpace(output)
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		return FormatCommandFailureResult(exitError.ExitCode(), output)
	}
	output = strings.TrimSpace(output)
	if output == "" {
		return fmt.Sprintf("Command execution failed: %v", err)
	}
	if strings.HasPrefix(output, "Command execution failed:") {
		return output
	}
	return fmt.Sprintf("Command execution failed: %v\nOutput: %s", err, output)
}

// ExecuteFailureStatusLine is the single-line status appended at the end of streaming execute (output body was already pushed in the stream).
func ExecuteFailureStatusLine(exitCode int) string {
	return fmt.Sprintf("\nCommand execution failed: exit status %d", exitCode)
}

// IsCommandFailureResult checks whether the tool result body indicates a non-zero exit (used by execute / exec to align isError).
func IsCommandFailureResult(content string) bool {
	return strings.Contains(content, "Command execution failed:")
}

// IsLegacyShellExitNoise filters out redundant exit code lines in legacy shell streams.
func IsLegacyShellExitNoise(s string) bool {
	trimmed := strings.TrimSpace(s)
	return strings.HasPrefix(trimmed, "command exited with non-zero code ")
}
