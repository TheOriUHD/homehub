package assistant

import (
	"regexp"
	"strings"
)

// ModelAuto is the picker sentinel: route this turn by rule instead of
// pinning one model.
const ModelAuto = "auto"

// A lane is a role, not a model. Each provider names one model per lane, so
// the same rules route Anthropic, OpenAI, and a pair of local endpoints
// without knowing anything about them.
type Lane string

const (
	// LaneBasic answers lookups and carries out named commands — the
	// receptionist. Fast and cheap; it is the default.
	LaneBasic Lane = "basic"
	// LaneAdvanced answers questions that need interpretation: why something
	// is the way it is, what the data does not say on its face, what to do
	// about it.
	LaneAdvanced Lane = "advanced"
)

// Routing is one turn's decision, kept so the answer can show what decided it.
type Routing struct {
	Lane Lane
	// Rule names the pattern that escalated the turn, empty when nothing did.
	// This is the whole point of rule-based routing: the reason is a fact we
	// can print, not a model's opinion of its own confidence.
	Rule string
}

// escalation is one named reason to leave the basic lane.
type escalation struct {
	rule string
	re   *regexp.Regexp
}

// Escalations are matched against the user's message, lowercased. They are
// deliberately narrow: the cost of missing one is a slightly worse answer the
// user can retry on a bigger model, while the cost of matching everything is
// paying advanced rates to turn a light on.
//
// German is covered alongside English because the app ships both.
var escalations = []escalation{
	{
		// Asking for a cause or an explanation, not a value.
		rule: "asks why",
		re: regexp.MustCompile(`\b(why|how come|what caused|what's causing|whats causing|` +
			`root cause|explain|reason for|wieso|warum|weshalb|erklär)`),
	},
	{
		// Pushing back on an answer already given — the follow-up in
		// "…but WHY is it like that".
		rule: "challenges the answer",
		re: regexp.MustCompile(`\b(are you sure|that's wrong|thats wrong|that is wrong|incorrect|` +
			`doesn't tell|doesnt tell|not what i|really\?|the truth|actually|` +
			`stimmt nicht|sicher\?|falsch)`),
	},
	{
		// Judgement: weigh things up, recommend, decide.
		rule: "asks for judgement",
		re: regexp.MustCompile(`\b(should i|do you think|recommend|advice|advise|best way|` +
			`worth it|compare|trade-?off|pros and cons|which one|better to|` +
			`empfiehlst|sollte ich|lohnt sich)`),
	},
	{
		// Diagnosis and analysis over raw retrieval.
		rule: "asks for diagnosis",
		re: regexp.MustCompile(`\b(diagnos|troubleshoot|debug|investigate|analyse|analyze|` +
			`figure out|get to the bottom|what does it mean|what do you make of|` +
			`correlat|unusual|suspicious|analysier|untersuch|fehlersuche)`),
	},
	{
		// Planning and writing, which need more than a lookup.
		rule: "asks to plan or write",
		re: regexp.MustCompile(`\b(plan |write me|draft|design a|come up with|strategy|` +
			`step by step|walk me through|entwirf|schreib mir)`),
	},
}

// longMessageWords escalates on sheer size: a question this long is carrying
// context that a receptionist model will flatten.
const longMessageWords = 60

// ClassifyLane decides which lane answers a message. It looks ONLY at the
// text — no model is asked how hard it finds the question, because a model's
// self-assessment is exactly the thing that cannot be audited or predicted.
func ClassifyLane(text string) Routing {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return Routing{Lane: LaneBasic}
	}
	for _, e := range escalations {
		if e.re.MatchString(lower) {
			return Routing{Lane: LaneAdvanced, Rule: e.rule}
		}
	}
	if len(strings.Fields(lower)) >= longMessageWords {
		return Routing{Lane: LaneAdvanced, Rule: "long question"}
	}
	return Routing{Lane: LaneBasic}
}

// LaneModel returns the model a provider uses for a lane. Catalog providers
// declare it on their entries; catalog-less ones (local runtimes, OpenRouter)
// take the two model names an administrator configured, which is why both are
// passed in rather than read from the registry.
func LaneModel(providerID string, lane Lane, configuredBasic, configuredAdvanced string) string {
	configured := strings.TrimSpace(configuredBasic)
	if lane == LaneAdvanced {
		configured = strings.TrimSpace(configuredAdvanced)
	}
	if configured != "" {
		return configured
	}
	info := ProviderByID(providerID)
	if info == nil {
		return ""
	}
	for _, m := range info.Models {
		if m.AutoLane == string(lane) {
			return m.ID
		}
	}
	return ""
}

// AutoAvailable reports whether Auto can route for a provider at all — it
// needs a model for BOTH lanes, or it is not a choice, just a default.
func AutoAvailable(providerID, configuredBasic, configuredAdvanced string) bool {
	return LaneModel(providerID, LaneBasic, configuredBasic, configuredAdvanced) != "" &&
		LaneModel(providerID, LaneAdvanced, configuredBasic, configuredAdvanced) != ""
}
