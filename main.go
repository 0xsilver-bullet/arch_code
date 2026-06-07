package main

import (
	"arch_code/internal/core"
	"arch_code/internal/tools"
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/joho/godotenv"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

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

	client := openai.NewClient(
		option.WithBaseURL("https://ollama.com/v1"),
		option.WithAPIKey(ollamaApiKey),
	)

	messages := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage(core.SystemPrompt()),
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
				Tools:    []openai.ChatCompletionToolUnionParam{tools.ReadTool, tools.BashTool, tools.WriteTool, tools.EditTool, tools.MultiEditTool},
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
				result := tools.ExecToolCall(tc.Function.Name, tc.Function.Arguments, cwd)
				messages = append(messages, openai.ToolMessage(result, tc.ID))
			}
			// Loop back — send tool results to the model for the next turn
		}
	}
}
