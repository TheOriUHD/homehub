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
	// Models is the curated switch-on-the-fly menu. Empty means the provider
	// has no fixed catalog (local runtimes, OpenRouter's thousands of paths):
	// those keep the free-text model field and no picker.
	Models []ModelInfo `json:"models,omitempty"`
}

// Cost tiers gate the picker. They describe SPEND, not capability — a model
// is gated because picking it by accident is expensive, and the confirmation
// text names the multiple so the number is never a vague "this costs more".
const (
	// TierStandard needs no confirmation.
	TierStandard = ""
	// TierPremium asks once before switching.
	TierPremium = "premium"
	// TierUltra asks and makes you wait, so the price registers.
	TierUltra = "ultra"
)

// ModelInfo is one entry in a provider's model picker.
type ModelInfo struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Blurb string `json:"blurb"`
	Tier  string `json:"tier,omitempty"`
	// CostNote is the warning body for a gated model, phrased as a ratio
	// against the model below it — ratios age far better than dollar figures.
	CostNote string `json:"costNote,omitempty"`
	// Aliases are other ids that mean this model, so a pinned dated snapshot
	// already in the settings still lights up its own row in the picker.
	Aliases []string `json:"aliases,omitempty"`
	// AutoLane marks this model as a provider's basic or advanced lane for
	// rule-based Auto routing. At most one model per lane per provider.
	AutoLane string `json:"autoLane,omitempty"`
	// List price in US dollars per million tokens. Used only to estimate what
	// an answer cost — cached input and promotional rates bill lower, so the
	// figure shown to the user is labelled an estimate.
	InputPerMTok  float64 `json:"inputPerMTok,omitempty"`
	OutputPerMTok float64 `json:"outputPerMTok,omitempty"`
}

// Providers is the registry, in display order: cloud APIs first, then
// local runtimes. Base URLs are editable everywhere; the defaults are each
// runtime's documented endpoint.
var Providers = []ProviderInfo{
	{ID: "anthropic", Label: "Anthropic", Kind: "cloud", Engine: "anthropic", RequiresKey: true,
		DefaultModel: "claude-sonnet-5", ModelHint: "e.g. claude-sonnet-5, claude-opus-5",
		Models: AnthropicModels},
	{ID: "openai", Label: "OpenAI", Kind: "cloud", Engine: "openai", RequiresKey: true,
		DefaultModel: "gpt-5.6-terra", DefaultBaseURL: "https://api.openai.com/v1",
		ModelHint: "e.g. gpt-5.6-terra, gpt-5.6-sol", Models: OpenAIModels},
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

// AnthropicModels is the Claude picker, cheapest first. Only Haiku carries a
// dated snapshot id; the 5-family ids are complete as written.
var AnthropicModels = []ModelInfo{
	{ID: "claude-haiku-4-5", Label: "Haiku 4.5", Blurb: "Fastest and cheapest. Fine for lookups and status questions.",
		Aliases: []string{"claude-haiku-4-5-20251001"}, AutoLane: "basic", InputPerMTok: 1, OutputPerMTok: 5},
	{ID: "claude-sonnet-5", Label: "Sonnet 5", Blurb: "The balanced default — strong reasoning at a moderate price.",
		AutoLane: "advanced", InputPerMTok: 2, OutputPerMTok: 10},
	{ID: "claude-opus-5", Label: "Opus 5", Blurb: "Deeper reasoning for hard diagnosis.", Tier: TierPremium,
		CostNote: "About 2.5× the price of Sonnet 5 per token.", InputPerMTok: 5, OutputPerMTok: 25},
	{ID: "claude-fable-5", Label: "Fable 5", Blurb: "The most capable model, for the hardest problems.", Tier: TierUltra,
		CostNote: "Twice the price of Opus 5 — about 5× Sonnet 5 — per token.", InputPerMTok: 10, OutputPerMTok: 50},
}

// OpenAIModels is the GPT picker, cheapest first.
var OpenAIModels = []ModelInfo{
	{ID: "gpt-5.6-luna", Label: "GPT-5.6 Luna", Blurb: "Cost-optimised. Fine for lookups and status questions.",
		AutoLane: "basic", InputPerMTok: 0.20, OutputPerMTok: 1.20},
	{ID: "gpt-5.6-terra", Label: "GPT-5.6 Terra", Blurb: "The balanced default — intelligence against cost.",
		AutoLane: "advanced", InputPerMTok: 2, OutputPerMTok: 12},
	{ID: "gpt-5.6-sol", Label: "GPT-5.6 Sol", Blurb: "Frontier model for complex professional work.", Tier: TierPremium,
		CostNote: "About 2.5× the price of Terra per token.", InputPerMTok: 5, OutputPerMTok: 30},
}

// ResolveModel accepts a model only when the configured provider actually
// offers it. A picker choice arrives from the browser, so an unchecked string
// would let anyone point the key at any model the vendor sells.
func ResolveModel(providerID, model string) (string, bool) {
	info := ProviderByID(providerID)
	if info == nil {
		return "", false
	}
	for _, m := range info.Models {
		if m.ID == model {
			return m.ID, true
		}
		for _, alias := range m.Aliases {
			if alias == model {
				return m.ID, true
			}
		}
	}
	return "", false
}

// ModelInfoFor finds the catalog entry for a model id (aliases included) on
// any provider, so a stored answer can be priced even after the instance has
// switched providers.
func ModelInfoFor(model string) *ModelInfo {
	for i := range Providers {
		for j := range Providers[i].Models {
			m := &Providers[i].Models[j]
			if m.ID == model {
				return m
			}
			for _, alias := range m.Aliases {
				if alias == model {
					return m
				}
			}
		}
	}
	return nil
}

// Prompt-cache multipliers against the base input rate. A read is nearly
// free; a write carries a premium that pays for itself on the second request.
const (
	cacheReadRate  = 0.10
	cacheWriteRate = 1.25
)

// EstimateCost prices a turn in US dollars from list rates, and reports
// whether the model is one we have prices for at all. Cached tokens are
// priced at their own rates — counting a cache read as full-price input
// would overstate every turn after the first by roughly the size of the
// conversation so far.
func EstimateCost(model string, u Usage) (float64, bool) {
	info := ModelInfoFor(model)
	if info == nil || (info.InputPerMTok == 0 && info.OutputPerMTok == 0) {
		return 0, false
	}
	billedInput := float64(u.InputTokens) +
		float64(u.CacheReadTokens)*cacheReadRate +
		float64(u.CacheWriteTokens)*cacheWriteRate
	return billedInput/1e6*info.InputPerMTok +
		float64(u.OutputTokens)/1e6*info.OutputPerMTok, true
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
