package main

import (
	"arch_code/tools"
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

const systemPrompt = `You are an expert coding assistant operating inside arch_code, a coding agent harness. You help users by answering questions, explaining code, and writing new code.

Available tools:
- read: Read file contents
- bash: Execute shell commands

Guidelines:
- Be concise in your responses
- Show file paths clearly when working with files
- When asked to read or analyze a file, always use the read tool

Current date: %s
Current working directory: %s`

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	ollamaApiKey := os.Getenv("OLLAMA_API_KEY")
	if ollamaApiKey == "" {
		log.Fatal("Ollama API key is required")
	}

	cwd, _ := os.Getwd()
	date := time.Now().Format("2006-01-02")

	client := openai.NewClient(
		option.WithBaseURL("https://ollama.com/v1"),
		option.WithAPIKey(ollamaApiKey),
	)

	messages := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage(fmt.Sprintf(systemPrompt, date, cwd)),
	}

	ctx := context.Background()

	scanner := bufio.NewScanner(os.Stdin)
	fmt.Println("Agent ready. Type your message (Ctrl+C to quit):")

	for {
		fmt.Print("\n> ")
		if !scanner.Scan() {
			break
		}
		userInput := strings.TrimSpace(scanner.Text())
		if userInput == "" {
			continue
		}

		messages = append(messages, openai.UserMessage(userInput))
		fmt.Println()

		// Agentic loop: keep going until the model gives a final text response
		for {
			stream := client.Chat.Completions.NewStreaming(ctx, openai.ChatCompletionNewParams{
				Model:    "gemma4:31b-cloud",
				Messages: messages,
				Tools:    []openai.ChatCompletionToolUnionParam{tools.ReadTool},
			})

			// acc stitches all streaming deltas into a complete ChatCompletion,
			// including full tool call metadata (IDs, names, accumulated arguments)
			acc := openai.ChatCompletionAccumulator{}

			for stream.Next() {
				chunk := stream.Current()
				acc.AddChunk(chunk)

				// Stream text tokens to the user in real time
				if len(chunk.Choices) > 0 {
					fmt.Print(chunk.Choices[0].Delta.Content)
				}
			}

			if err := stream.Err(); err != nil {
				fmt.Printf("\nStream error: %v\n", err)
				break
			}

			if len(acc.Choices) == 0 {
				fmt.Println()
				break
			}

			choice := acc.Choices[0]

			// ToParam() produces ChatCompletionAssistantMessageParam with the full
			// ToolCalls []ChatCompletionMessageToolCallUnionParam populated —
			// this is what makes the conversation history valid on the next turn
			messages = append(messages, choice.Message.ToParam())

			// No tool calls → final text answer, exit inner loop
			if len(choice.Message.ToolCalls) == 0 {
				fmt.Println()
				break
			}

			// Execute each tool call and feed results back
			for _, tc := range choice.Message.ToolCalls {
				fmt.Printf("\n[tool: %s %s]\n", tc.Function.Name, tc.Function.Arguments)

				var result string
				switch tc.Function.Name {
				case "read":
					var p tools.ReadParams
					if err := json.Unmarshal([]byte(tc.Function.Arguments), &p); err != nil {
						result = fmt.Sprintf("Error parsing arguments: %v", err)
					} else {
						result = tools.ExecuteRead(cwd, p)
					}

				default:
					result = fmt.Sprintf("Unknown tool: %s", tc.Function.Name)
				}

				messages = append(messages, openai.ToolMessage(result, tc.ID))
			}
			// Loop back — send tool results to the model for the next turn
		}
	}
}
