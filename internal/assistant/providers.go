package assistant

import "fmt"

// ProviderInfo describes one selectable provider. Everything except
// Anthropic and Gemini speaks the OpenAI Chat Completions dialect, so the
// engine field routes to one of three protocol implementations while the
// registry carries per-provider defaults.
type ProviderInfo struct {
	ID             string `json:"id"`
	Label          string `json:"label"`
	Kind           string `json:"kind"` // cloud | local
	Engine         string `json:"-"`    // anthropic | gemini | openai
	RequiresKey    bool   `json:"requiresKey"`
	DefaultModel   string `json:"defaultModel"`
	DefaultBaseURL string `json:"defaultBaseUrl,omitempty"`
	ModelHint      string `json:"modelHint,omitempty"`
}

// Providers is the registry, in display order: cloud APIs first, then
// local runtimes. Base URLs are editable everywhere; the defaults are each
// runtime's documented endpoint.
var Providers = []ProviderInfo{
	{ID: "anthropic", Label: "Anthropic", Kind: "cloud", Engine: "anthropic", RequiresKey: true,
		DefaultModel: "claude-sonnet-5", ModelHint: "e.g. claude-sonnet-5, claude-opus-5"},
	{ID: "openai", Label: "OpenAI", Kind: "cloud", Engine: "openai", RequiresKey: true,
		DefaultModel: "gpt-5", DefaultBaseURL: "https://api.openai.com/v1", ModelHint: "e.g. gpt-5, gpt-5-mini"},
	{ID: "google", Label: "Google Gemini", Kind: "cloud", Engine: "gemini", RequiresKey: true,
		DefaultModel: "gemini-2.5-flash", ModelHint: "e.g. gemini-2.5-pro, gemini-2.5-flash"},
	{ID: "mistral", Label: "Mistral", Kind: "cloud", Engine: "openai", RequiresKey: true,
		DefaultModel: "mistral-large-latest", DefaultBaseURL: "https://api.mistral.ai/v1"},
	{ID: "xai", Label: "xAI Grok", Kind: "cloud", Engine: "openai", RequiresKey: true,
		DefaultModel: "grok-4", DefaultBaseURL: "https://api.x.ai/v1"},
	{ID: "deepseek", Label: "DeepSeek", Kind: "cloud", Engine: "openai", RequiresKey: true,
		DefaultModel: "deepseek-chat", DefaultBaseURL: "https://api.deepseek.com/v1"},
	{ID: "groq", Label: "Groq", Kind: "cloud", Engine: "openai", RequiresKey: true,
		DefaultModel: "llama-3.3-70b-versatile", DefaultBaseURL: "https://api.groq.com/openai/v1"},
	{ID: "openrouter", Label: "OpenRouter", Kind: "cloud", Engine: "openai", RequiresKey: true,
		DefaultModel: "anthropic/claude-sonnet-4.5", DefaultBaseURL: "https://openrouter.ai/api/v1",
		ModelHint: "any OpenRouter model path"},
	{ID: "together", Label: "Together AI", Kind: "cloud", Engine: "openai", RequiresKey: true,
		DefaultModel: "meta-llama/Llama-3.3-70B-Instruct-Turbo", DefaultBaseURL: "https://api.together.xyz/v1"},
	{ID: "fireworks", Label: "Fireworks AI", Kind: "cloud", Engine: "openai", RequiresKey: true,
		DefaultModel: "accounts/fireworks/models/llama-v3p3-70b-instruct", DefaultBaseURL: "https://api.fireworks.ai/inference/v1"},

	{ID: "ollama", Label: "Ollama (local)", Kind: "local", Engine: "openai", RequiresKey: false,
		DefaultModel: "qwen3:8b", DefaultBaseURL: "http://127.0.0.1:11434/v1",
		ModelHint: "a pulled model that supports tools, e.g. qwen3:8b, llama3.3"},
	{ID: "lmstudio", Label: "LM Studio (local)", Kind: "local", Engine: "openai", RequiresKey: false,
		DefaultModel: "local-model", DefaultBaseURL: "http://127.0.0.1:1234/v1",
		ModelHint: "the model identifier shown in LM Studio"},
	{ID: "llamacpp", Label: "llama.cpp server (local)", Kind: "local", Engine: "openai", RequiresKey: false,
		DefaultModel: "default", DefaultBaseURL: "http://127.0.0.1:8080/v1",
		ModelHint: "llama.cpp serves one model; the name is informational"},
	{ID: "vllm", Label: "vLLM (local)", Kind: "local", Engine: "openai", RequiresKey: false,
		DefaultModel: "Qwen/Qwen3-8B", DefaultBaseURL: "http://127.0.0.1:8000/v1",
		ModelHint: "the --model the server was started with"},
	{ID: "localai", Label: "LocalAI (local)", Kind: "local", Engine: "openai", RequiresKey: false,
		DefaultModel: "gpt-4", DefaultBaseURL: "http://127.0.0.1:8080/v1",
		ModelHint: "an installed LocalAI model name"},
	{ID: "custom", Label: "Custom (OpenAI-compatible)", Kind: "local", Engine: "openai", RequiresKey: false,
		DefaultModel: "", DefaultBaseURL: "http://127.0.0.1:8000/v1",
		ModelHint: "whatever your endpoint serves"},
}

// ProviderByID looks a provider up, or nil.
func ProviderByID(id string) *ProviderInfo {
	for i := range Providers {
		if Providers[i].ID == id {
			return &Providers[i]
		}
	}
	return nil
}

// NewProviderFor builds the protocol client for a configuration. BaseURL
// falls back to the registry default; the key may be empty for local
// providers.
func NewProviderFor(cfg Config) (Provider, error) {
	info := ProviderByID(cfg.Provider)
	if info == nil {
		return nil, fmt.Errorf("unknown AI provider %q", cfg.Provider)
	}
	base := cfg.BaseURL
	if base == "" {
		base = info.DefaultBaseURL
	}
	switch info.Engine {
	case "anthropic":
		return &AnthropicProvider{APIKey: cfg.APIKey, BaseURL: cfg.BaseURL}, nil
	case "gemini":
		return &GeminiProvider{APIKey: cfg.APIKey, BaseURL: cfg.BaseURL}, nil
	case "openai":
		return &OpenAIProvider{APIKey: cfg.APIKey, BaseURL: base}, nil
	default:
		return nil, fmt.Errorf("provider %q has no engine", cfg.Provider)
	}
}
