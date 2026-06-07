package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/openai/openai-go/v3"
)

type WriteParams struct {
	FilePath string `json:"file_path"`
	Content  string `json:"content"`
}

func ExecuteWrite(cwd string, p WriteParams) string {
	if p.FilePath == "" {
		return "Error: file_path is required"
	}

	filePath := p.FilePath
	if !filepath.IsAbs(filePath) {
		filePath = filepath.Join(cwd, filePath)
	}

	fileInfo, err := os.Stat(filePath)
	if err == nil {
		if fileInfo.IsDir() {
			return fmt.Sprintf("Error: path is a directory, not a file: %s", filePath)
		}

		modTime := fileInfo.ModTime().Truncate(time.Second)
		lastRead := LastReadTime(filePath)
		if modTime.After(lastRead) {
			return fmt.Sprintf(
				"Error: file %s has been modified since it was last read.\nLast modification: %s\nLast read: %s\n\nPlease read the file again before modifying it.",
				filePath, modTime.Format(time.RFC3339), lastRead.Format(time.RFC3339),
			)
		}

		oldBytes, readErr := os.ReadFile(filePath)
		if readErr == nil && string(oldBytes) == p.Content {
			return fmt.Sprintf("Error: file %s already contains the exact content. No changes made.", filePath)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Sprintf("Error: checking file: %v", err)
	}

	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Sprintf("Error: creating directory: %v", err)
	}

	if err := os.WriteFile(filePath, []byte(p.Content), 0o644); err != nil {
		return fmt.Sprintf("Error: writing file: %v", err)
	}

	RecordRead(filePath)
	return fmt.Sprintf("<result>\nFile successfully written: %s\n</result>", filePath)
}

var WriteTool = openai.ChatCompletionToolUnionParam{
	OfFunction: &openai.ChatCompletionFunctionToolParam{
		Function: openai.FunctionDefinitionParam{
			Name:        "write",
			Description: openai.String("Create or overwrite a file with given content; auto-creates parent dirs. Cannot append. Read the file first to avoid conflicts. For surgical changes use edit or multiedit."),
			Parameters: openai.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"file_path": map[string]any{
						"type":        "string",
						"description": "The path to the file to write. Relative paths are resolved from the current working directory.",
					},
					"content": map[string]any{
						"type":        "string",
						"description": "The content to write to the file.",
					},
				},
				"required": []string{"file_path", "content"},
			},
		},
	},
}
