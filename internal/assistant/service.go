package assistant

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/TheOriUHD/homehub/internal/store"
)

// maxToolRounds bounds one turn's tool loop; a model that keeps asking for
// tools past this gets cut off with whatever it has.
const maxToolRounds = 6

// historyWindow is how many stored messages ride along as context.
const historyWindow = 30

// Config is the runtime assistant configuration resolved by the caller
// (settings + vault) per turn — nothing is cached across turns, so a key
// or model change applies immediately.
type Config struct {
	Provider string
	Model    string
	APIKey   string
	BaseURL  string
}

// StepInfo records one tool invocation for the transparency trace.
type StepInfo struct {
	Tool    string          `json:"tool"`
	Input   json.RawMessage `json:"input,omitempty"`
	IsError bool            `json:"isError,omitempty"`
}

// Service runs assistant turns.
type Service struct {
	Store   *store.Store
	Toolbox *Toolbox
}

func systemPrompt(displayName string) string {
	return strings.TrimSpace(`
You are the HomeHub assistant, built into a self-hosted homelab control plane.
You are talking to ` + displayName + `.
Use the tools to answer from live data — never invent numbers, names, or states.
The tools already enforce the user's permissions; if one refuses, say so plainly.
Answer concisely and concretely. Use short sentences. When a value has a unit, include it.
If something looks unhealthy (a system unreachable, an alert firing), lead with it.
You also have CONTROL tools: Home Assistant services, Docker container lifecycle,
Minecraft console commands, AMP and Proxmox guest lifecycle, DNS blocking. Every action
runs under the user's own permissions and is audited. When the user gives an explicit
instruction, act. For disruptive actions the user did NOT explicitly ask for
(stopping or rebooting VMs, containers, or game servers), ask before acting.
After an action, confirm what happened from the tool result — do not assume; if a tool
result shows an error or a failed console reaction, say the action failed and why.
You do NOT have: SSH or shell access, file access, or lifecycle control of standalone
Minecraft servers (only AMP instances, Docker containers, and Proxmox guests can be
started/stopped). When a request needs a capability you lack, say so plainly and
suggest where the user can do it — never offer to attempt it anyway.
When a command needs an in-game or device name you don't know, look it up with the
detail tools before asking the user.`)
}

// RunTurn executes one user message: history in, tool loop, reply out.
// Both sides of the exchange are persisted before returning.
func (s *Service) RunTurn(ctx context.Context, provider Provider, cfg Config, access Access, text string) (string, []StepInfo, error) {
	history, err := s.Store.ListAssistantMessages(ctx, access.UserID, historyWindow)
	if err != nil {
		return "", nil, err
	}
	turns := make([]Turn, 0, len(history)+1)
	for _, msg := range history {
		turns = append(turns, Turn{Role: msg.Role, Text: msg.Content})
	}
	turns = append(turns, Turn{Role: "user", Text: text})

	if err := s.Store.AddAssistantMessage(ctx, access.UserID, "user", text, nil); err != nil {
		return "", nil, err
	}

	defs := s.Toolbox.Defs()
	var steps []StepInfo
	var reply Reply
	for round := 0; ; round++ {
		reply, err = provider.Chat(ctx, cfg.Model, systemPrompt(access.DisplayName), turns, defs)
		if err != nil {
			return "", steps, err
		}
		if len(reply.Calls) == 0 || round >= maxToolRounds {
			break
		}
		turns = append(turns, Turn{Role: "assistant", Text: reply.Text, Calls: reply.Calls})
		results := make([]ToolResult, 0, len(reply.Calls))
		for _, call := range reply.Calls {
			out, runErr := s.Toolbox.Run(ctx, access, call.Name, call.Input)
			step := StepInfo{Tool: call.Name, Input: call.Input}
			if runErr != nil {
				step.IsError = true
				results = append(results, ToolResult{ID: call.ID, Content: runErr.Error(), IsError: true})
			} else {
				results = append(results, ToolResult{ID: call.ID, Content: out})
			}
			steps = append(steps, step)
		}
		turns = append(turns, Turn{Role: "user", Results: results})
	}

	final := strings.TrimSpace(reply.Text)
	if final == "" {
		final = "(no reply)"
	}
	var trace json.RawMessage
	if len(steps) > 0 {
		trace, _ = json.Marshal(steps)
	}
	if err := s.Store.AddAssistantMessage(ctx, access.UserID, "assistant", final, trace); err != nil {
		return "", steps, err
	}
	return final, steps, nil
}
