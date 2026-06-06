package tools

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/openai/openai-go/v3"
)

const (
	bashMaxLines = 2000
	bashMaxBytes = 50 * 1024 // 50KB
)

type BashParams struct {
	Command string   `json:"command"`
	Timeout *float64 `json:"timeout,omitempty"`
}

type truncationResult struct {
	Content          string
	Truncated        bool
	TruncatedBy      string // "lines", "bytes", or ""
	TotalLines       int
	TotalBytes       int
	OutputLines      int
	OutputBytes      int
	LastLinePartial  bool
	FirstLineExceeds bool
	MaxLines         int
	MaxBytes         int
}

type bashResult struct {
	Output         string
	ExitCode       *int
	Cancelled      bool
	Truncated      bool
	FullOutputPath string
}

func truncateTail(content string, maxLines int, maxBytes int) truncationResult {
	if content == "" {
		return truncationResult{
			Content:     "",
			Truncated:   false,
			TruncatedBy: "",
			TotalLines:  0,
			TotalBytes:  0,
			OutputLines: 0,
			OutputBytes: 0,
			MaxLines:    maxLines,
			MaxBytes:    maxBytes,
		}
	}
	totalBytes := len([]byte(content))
	lines := strings.Split(content, "\n")
	if content[len(content)-1] == '\n' {
		lines = lines[:len(lines)-1]
	}
	totalLines := len(lines)

	if totalLines <= maxLines && totalBytes <= maxBytes {
		return truncationResult{
			Content:     content,
			Truncated:   false,
			TruncatedBy: "",
			TotalLines:  totalLines,
			TotalBytes:  totalBytes,
			OutputLines: totalLines,
			OutputBytes: totalBytes,
			MaxLines:    maxLines,
			MaxBytes:    maxBytes,
		}
	}

	// Work backwards from the end
	var outputLines []string
	outputBytesCount := 0
	truncatedBy := "lines"
	lastLinePartial := false

	for i := totalLines - 1; i >= 0 && len(outputLines) < maxLines; i-- {
		line := lines[i]
		lineBytes := len([]byte(line))
		if len(outputLines) > 0 {
			lineBytes++ // +1 for newline
		}

		if outputBytesCount+lineBytes > maxBytes {
			truncatedBy = "bytes"
			// Edge case: if we haven't added ANY lines yet and this line exceeds maxBytes,
			// take the end of the line (partial)
			if len(outputLines) == 0 {
				truncatedLine := truncateStringToBytesFromEnd(line, maxBytes)
				outputLines = append([]string{truncatedLine}, outputLines...)
				outputBytesCount = len([]byte(truncatedLine))
				lastLinePartial = true
			}
			break
		}

		outputLines = append([]string{line}, outputLines...)
		outputBytesCount += lineBytes
	}

	if len(outputLines) >= maxLines && outputBytesCount <= maxBytes {
		truncatedBy = "lines"
	}

	outputContent := strings.Join(outputLines, "\n")
	finalOutputBytes := len([]byte(outputContent))

	return truncationResult{
		Content:          outputContent,
		Truncated:        true,
		TruncatedBy:      truncatedBy,
		TotalLines:       totalLines,
		TotalBytes:       totalBytes,
		OutputLines:      len(outputLines),
		OutputBytes:      finalOutputBytes,
		LastLinePartial:  lastLinePartial,
		FirstLineExceeds: false,
		MaxLines:         maxLines,
		MaxBytes:         maxBytes,
	}
}

func truncateStringToBytesFromEnd(s string, maxBytes int) string {
	buf := []byte(s)
	if len(buf) <= maxBytes {
		return s
	}

	start := len(buf) - maxBytes
	// Find valid UTF-8 boundary
	for start < len(buf) && (buf[start]&0xC0) == 0x80 {
		start++
	}

	return string(buf[start:])
}

var ansiRegex = regexp.MustCompile("\x1b(?:\\[[0-9;]*[a-zA-Z]|[\\]\\\\^_]|\\][0-9;]*[^\x07]*\x07|P[^\x07]*\x07)")

func stripAnsi(s string) string {
	return ansiRegex.ReplaceAllString(s, "")
}

func sanitizeBinaryOutput(s string) string {
	var buf strings.Builder
	buf.Grow(len(s))
	for _, r := range s {
		if r == 0x09 || r == 0x0A || r == 0x0D {
			buf.WriteRune(r)
			continue
		}
		if r <= 0x1F {
			continue
		}
		if r >= 0xFFF9 && r <= 0xFFFB {
			continue
		}
		buf.WriteRune(r)
	}
	return buf.String()
}

func getShellConfig() (shell string, args []string) {
	if customShell := os.Getenv("ARCH_CODE_SHELL_PATH"); customShell != "" {
		if _, err := os.Stat(customShell); err == nil {
			return customShell, []string{"-c"}
		}
	}

	if runtime.GOOS == "windows" {
		// Try Git Bash locations
		for _, p := range []string{
			filepath.Join(os.Getenv("ProgramFiles"), "Git", "bin", "bash.exe"),
			filepath.Join(os.Getenv("ProgramFiles(x86)"), "Git", "bin", "bash.exe"),
		} {
			if _, err := os.Stat(p); err == nil {
				return p, []string{"-c"}
			}
		}
		// Try bash on PATH
		if p, err := exec.LookPath("bash.exe"); err == nil {
			return p, []string{"-c"}
		}
		// Try sh on PATH
		if p, err := exec.LookPath("sh.exe"); err == nil {
			return p, []string{"-c"}
		}
		panic("no bash shell found on Windows")
	}

	// Unix: try /bin/bash first
	if _, err := os.Stat("/bin/bash"); err == nil {
		return "/bin/bash", []string{"-c"}
	}
	if p, err := exec.LookPath("bash"); err == nil {
		return p, []string{"-c"}
	}
	// Fallback to sh
	return "sh", []string{"-c"}
}

func formatSize(bytes int64) string {
	if bytes < 1024 {
		return fmt.Sprintf("%dB", bytes)
	} else if bytes < 1024*1024 {
		return fmt.Sprintf("%.1fKB", float64(bytes)/1024)
	} else {
		return fmt.Sprintf("%.1fMB", float64(bytes)/(1024*1024))
	}
}

func ExecuteBash(cwd string, p BashParams) string {
	shell, shellArgs := getShellConfig()

	// Verify working directory exists
	if _, err := os.Stat(cwd); err != nil {
		return fmt.Sprintf("Error: working directory does not exist: %s\nCannot execute bash commands.", cwd)
	}

	ctx := context.Background()
	var cancel context.CancelFunc
	if p.Timeout != nil && *p.Timeout > 0 {
		timeout := time.Duration(*p.Timeout * float64(time.Second))
		ctx, cancel = context.WithTimeout(ctx, timeout)
	} else {
		ctx, cancel = context.WithCancel(ctx)
	}
	defer cancel()

	cmd := exec.CommandContext(ctx, shell, append(shellArgs, p.Command)...)
	cmd.Dir = cwd
	cmd.Env = os.Environ()
	cmd.Stdin = nil

	// Set process group for killing process tree (Unix)
	if runtime.GOOS != "windows" {
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	}

	var stdoutBuf, stderrBuf bytes.Buffer
	var combinedOutput bytes.Buffer
	multiWriter := io.MultiWriter(&combinedOutput, &stdoutBuf)
	cmd.Stdout = multiWriter
	cmd.Stderr = io.MultiWriter(&combinedOutput, &stderrBuf)

	err := cmd.Run()

	// Read the full output
	rawOutput := combinedOutput.String()

	// Sanitize output: strip ANSI, sanitize binary, normalize \r\n to \n
	cleanOutput := sanitizeBinaryOutput(stripAnsi(rawOutput))
	cleanOutput = strings.ReplaceAll(cleanOutput, "\r\n", "\n")
	cleanOutput = strings.ReplaceAll(cleanOutput, "\r", "")

	// Check for cancellation / timeout
	if ctx.Err() != nil {
		if ctx.Err() == context.DeadlineExceeded {
			timeoutSecs := 0.0
			if p.Timeout != nil {
				timeoutSecs = *p.Timeout
			}
			trunc := truncateTail(cleanOutput, bashMaxLines, bashMaxBytes)
			result := trunc.Content
			if result != "" {
				result += "\n\n"
			}
			result += fmt.Sprintf("Command timed out after %.0f seconds", timeoutSecs)
			return result
		}
		// Context cancelled
		trunc := truncateTail(cleanOutput, bashMaxLines, bashMaxBytes)
		result := trunc.Content
		if result != "" {
			result += "\n\n"
		}
		result += "Command aborted"
		return result
	}

	if err != nil {
		// Get exit code
		exitCode := 1
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}

		trunc := truncateTail(cleanOutput, bashMaxLines, bashMaxBytes)
		result := trunc.Content
		if result != "" {
			result += "\n\n"
		}
		result += fmt.Sprintf("Command exited with code %d", exitCode)

		if trunc.Truncated {
			result += "\n"
			if trunc.TruncatedBy == "lines" {
				result += fmt.Sprintf("\n[Showing lines %d-%d of %d.", trunc.TotalLines-trunc.OutputLines+1, trunc.TotalLines, trunc.TotalLines)
			} else {
				result += fmt.Sprintf("\n[Showing lines %d-%d of %d (%dKB limit).", trunc.TotalLines-trunc.OutputLines+1, trunc.TotalLines, trunc.TotalLines, bashMaxBytes/1024)
			}
			// Write full output to temp file
			tmpFile, tmpErr := writeFullOutputToTemp(cleanOutput)
			if tmpErr == nil {
				result += fmt.Sprintf(" Full output: %s]", tmpFile)
			} else {
				result += "]"
			}
		}
		return result
	}

	// Success (exit code 0)
	trunc := truncateTail(cleanOutput, bashMaxLines, bashMaxBytes)
	result := trunc.Content
	if result == "" {
		result = "(no output)"
	}

	if trunc.Truncated {
		result += "\n"
		if trunc.TruncatedBy == "lines" {
			result += fmt.Sprintf("\n[Showing lines %d-%d of %d.", trunc.TotalLines-trunc.OutputLines+1, trunc.TotalLines, trunc.TotalLines)
		} else {
			result += fmt.Sprintf("\n[Showing lines %d-%d of %d (%dKB limit).", trunc.TotalLines-trunc.OutputLines+1, trunc.TotalLines, trunc.TotalLines, bashMaxBytes/1024)
		}
		tmpFile, tmpErr := writeFullOutputToTemp(cleanOutput)
		if tmpErr == nil {
			result += fmt.Sprintf(" Full output: %s]", tmpFile)
		} else {
			result += "]"
		}
	}

	return result
}

func writeFullOutputToTemp(content string) (string, error) {
	tmpDir := os.TempDir()
	tmpFile, err := os.CreateTemp(tmpDir, "arch_code-bash-*.log")
	if err != nil {
		return "", err
	}
	defer tmpFile.Close()

	_, err = tmpFile.WriteString(content)
	if err != nil {
		return "", err
	}

	return tmpFile.Name(), nil
}

var BashTool = openai.ChatCompletionToolUnionParam{
	OfFunction: &openai.ChatCompletionFunctionToolParam{
		Function: openai.FunctionDefinitionParam{
			Name:        "bash",
			Description: openai.String("Execute a bash command in the current working directory. Returns stdout and stderr. Output is truncated to last 2000 lines or 50KB (whichever is hit first). If truncated, full output is saved to a temp file. Optionally provide a timeout in seconds."),
			Parameters: openai.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"command": map[string]any{
						"type":        "string",
						"description": "Bash command to execute",
					},
					"timeout": map[string]any{
						"type":        "number",
						"description": "Timeout in seconds (optional, no default timeout)",
					},
				},
				"required": []string{"command"},
			},
		},
	},
}
