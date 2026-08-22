package assistant

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// GeminiProvider speaks the Google Gemini generateContent API. Gemini has
// no tool-call IDs — calls and responses pair by function name and order,
// so the synthetic IDs used internally never cross the wire.
type GeminiProvider struct {
	APIKey  string
	BaseURL string // default https://generativelanguage.googleapis.com
	Client  *http.Client
}

type gemPart struct {
	Text         string `json:"text,omitempty"`
	FunctionCall *struct {
		Name string         `json:"name"`
		Args map[string]any `json:"args"`
	} `json:"functionCall,omitempty"`
	FunctionResponse *struct {
		Name     string         `json:"name"`
		Response map[string]any `json:"response"`
	} `json:"functionResponse,omitempty"`
}

type gemContent struct {
	Role  string    `json:"role"` // user | model
	Parts []gemPart `json:"parts"`
}

type gemRequest struct {
	SystemInstruction *gemContent  `json:"systemInstruction,omitempty"`
	Contents          []gemContent `json:"contents"`
	Tools             []struct {
		FunctionDeclarations []gemFunc `json:"functionDeclarations"`
	} `json:"tools,omitempty"`
}

type gemFunc struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type gemResponse struct {
	Candidates []struct {
		Content gemContent `json:"content"`
	} `json:"candidates"`
	UsageMetadata struct {
		PromptTokenCount     int64 `json:"promptTokenCount"`
		CandidatesTokenCount int64 `json:"candidatesTokenCount"`
	} `json:"usageMetadata"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (p *GeminiProvider) Chat(ctx context.Context, model, system string, turns []Turn, tools []ToolDef) (Reply, error) {
	// callNames maps our synthetic IDs back to function names for
	// functionResponse pairing.
	callNames := map[string]string{}
	for _, turn := range turns {
		for _, call := range turn.Calls {
			callNames[call.ID] = call.Name
		}
	}

	req := gemRequest{}
	if system != "" {
		req.SystemInstruction = &gemContent{Parts: []gemPart{{Text: system}}}
	}
	for _, turn := range turns {
		role := "user"
		if turn.Role == "assistant" {
			role = "model"
		}
		var parts []gemPart
		for _, res := range turn.Results {
			name := callNames[res.ID]
			payload := map[string]any{"result": res.Content}
			if res.IsError {
				payload = map[string]any{"error": res.Content}
			}
			part := gemPart{}
			part.FunctionResponse = &struct {
				Name     string         `json:"name"`
				Response map[string]any `json:"response"`
			}{Name: name, Response: payload}
			parts = append(parts, part)
		}
		if turn.Text != "" {
			parts = append(parts, gemPart{Text: turn.Text})
		}
		for _, call := range turn.Calls {
			var args map[string]any
			if json.Unmarshal(call.Input, &args) != nil || args == nil {
				args = map[string]any{}
			}
			part := gemPart{}
			part.FunctionCall = &struct {
				Name string         `json:"name"`
				Args map[string]any `json:"args"`
			}{Name: call.Name, Args: args}
			parts = append(parts, part)
		}
		if len(parts) == 0 {
			continue
		}
		req.Contents = append(req.Contents, gemContent{Role: role, Parts: parts})
	}
	if len(tools) > 0 {
		decl := struct {
			FunctionDeclarations []gemFunc `json:"functionDeclarations"`
		}{}
		for _, def := range tools {
			decl.FunctionDeclarations = append(decl.FunctionDeclarations, gemFunc{
				Name: def.Name, Description: def.Description, Parameters: def.InputSchema,
			})
		}
		req.Tools = append(req.Tools, decl)
	}

	body, err := json.Marshal(req)
	if err != nil {
		return Reply{}, err
	}
	base := p.BaseURL
	if base == "" {
		base = "https://generativelanguage.googleapis.com"
	}
	url := fmt.Sprintf("%s/v1beta/models/%s:generateContent", base, model)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return Reply{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-goog-api-key", p.APIKey)

	client := p.Client
	if client == nil {
		client = &http.Client{Timeout: 120 * time.Second}
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return Reply{}, fmt.Errorf("the AI provider could not be reached: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return Reply{}, err
	}
	var parsed gemResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return Reply{}, fmt.Errorf("the AI provider sent an unreadable response (HTTP %d)", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		if parsed.Error != nil && parsed.Error.Message != "" {
			return Reply{}, fmt.Errorf("the AI provider refused the request: %s", parsed.Error.Message)
		}
		return Reply{}, fmt.Errorf("the AI provider returned HTTP %d", resp.StatusCode)
	}
	if len(parsed.Candidates) == 0 {
		return Reply{}, fmt.Errorf("the AI provider sent no candidates")
	}

	var out Reply
	out.Usage = Usage{
		InputTokens:  parsed.UsageMetadata.PromptTokenCount,
		OutputTokens: parsed.UsageMetadata.CandidatesTokenCount,
	}
	callIdx := 0
	for _, part := range parsed.Candidates[0].Content.Parts {
		if part.Text != "" {
			if out.Text != "" {
				out.Text += "\n"
			}
			out.Text += part.Text
		}
		if part.FunctionCall != nil {
			input, _ := json.Marshal(part.FunctionCall.Args)
			callIdx++
			out.Calls = append(out.Calls, ToolCall{
				// Synthetic ID, ours alone: Gemini pairs by name/order.
				ID:    fmt.Sprintf("gem-%s-%d", part.FunctionCall.Name, callIdx),
				Name:  part.FunctionCall.Name,
				Input: input,
			})
		}
	}
	return out, nil
}
