import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Check, ChevronDown, Copy, Wand2 } from "lucide-react";
import { cn, copyText } from "@/lib/utils";
import { Button } from "@/ui/button";

/**
 * Setting up a local model, end to end: what to run on the machine that holds
 * the model, and what to put in the fields above afterwards.
 *
 * Two modes, because they are genuinely different jobs. One model is a single
 * server and one name. Auto mode routes each message between a quick model and
 * a capable one, and HomeHub aims both at ONE base URL — so a runtime that
 * serves a single model per process needs something in front of it. That
 * limitation is in the guide rather than left for the user to discover when
 * half their messages fail.
 *
 * Commands are not translated. They are code, and a localised flag is a
 * broken flag; only the prose around them has en/de keys.
 */

type Step = {
  /** Suffix of the locale key: assistant.guide.<provider>.<mode>.<id>.title|body */
  id: string;
  cmd?: string;
};

type SingleGuide = { steps: Step[]; baseUrl: string; model: string };
type AutoGuide = { steps: Step[]; baseUrl: string; basic: string; advanced: string };
type ProviderGuide = { single: SingleGuide; auto: AutoGuide };

// Model names, ports and file paths here are examples. They are real, current
// choices rather than placeholders — someone following this literally ends up
// with a working setup — but every one of them is meant to be swapped.
const GUIDES: Record<string, ProviderGuide> = {
  ollama: {
    single: {
      baseUrl: "http://192.168.1.50:11434/v1",
      model: "qwen3:8b",
      steps: [
        { id: "install", cmd: "curl -fsSL https://ollama.com/install.sh | sh" },
        { id: "pull", cmd: "ollama pull qwen3:8b" },
        {
          id: "expose",
          cmd: `sudo systemctl edit ollama

# paste this into the editor that opens, then save:
[Service]
Environment="OLLAMA_HOST=0.0.0.0:11434"

sudo systemctl restart ollama`,
        },
        { id: "check", cmd: "curl http://192.168.1.50:11434/v1/models" },
      ],
    },
    auto: {
      baseUrl: "http://192.168.1.50:11434/v1",
      basic: "qwen3:4b",
      advanced: "qwen3:14b",
      steps: [
        { id: "pullTwo", cmd: "ollama pull qwen3:4b\nollama pull qwen3:14b" },
        {
          id: "expose",
          cmd: `sudo systemctl edit ollama

# paste this into the editor that opens, then save:
[Service]
Environment="OLLAMA_HOST=0.0.0.0:11434"
Environment="OLLAMA_MAX_LOADED_MODELS=2"

sudo systemctl restart ollama`,
        },
        { id: "check", cmd: "curl http://192.168.1.50:11434/v1/models" },
      ],
    },
  },

  lmstudio: {
    single: {
      baseUrl: "http://192.168.1.50:1234/v1",
      model: "qwen/qwen3-8b",
      steps: [
        { id: "install" },
        { id: "serve" },
        { id: "load" },
        { id: "check", cmd: "curl http://192.168.1.50:1234/v1/models" },
      ],
    },
    auto: {
      baseUrl: "http://192.168.1.50:1234/v1",
      basic: "qwen/qwen3-4b",
      advanced: "qwen/qwen3-14b",
      steps: [
        { id: "downloadTwo" },
        { id: "serveBoth" },
        { id: "check", cmd: "curl http://192.168.1.50:1234/v1/models" },
      ],
    },
  },

  llamacpp: {
    single: {
      baseUrl: "http://192.168.1.50:8080/v1",
      model: "qwen3-8b",
      steps: [
        { id: "get" },
        {
          id: "serve",
          cmd: "llama-server -m ./qwen3-8b-q4_k_m.gguf --host 0.0.0.0 --port 8080 --jinja",
        },
        { id: "check", cmd: "curl http://192.168.1.50:8080/v1/models" },
      ],
    },
    auto: {
      baseUrl: "http://192.168.1.50:8080/v1",
      basic: "fast",
      advanced: "smart",
      steps: [
        { id: "oneModelEach" },
        {
          id: "swap",
          cmd: `# llama-swap.yaml — one port, two models, started on demand
models:
  "fast":
    cmd: llama-server -m /models/qwen3-4b-q4_k_m.gguf --port 8081 --jinja
  "smart":
    cmd: llama-server -m /models/qwen3-14b-q4_k_m.gguf --port 8082 --jinja`,
        },
        { id: "run", cmd: "llama-swap --listen 0.0.0.0:8080 --config llama-swap.yaml" },
        { id: "check", cmd: "curl http://192.168.1.50:8080/v1/models" },
      ],
    },
  },

  vllm: {
    single: {
      baseUrl: "http://192.168.1.50:8000/v1",
      model: "Qwen/Qwen3-8B",
      steps: [
        { id: "install", cmd: "pip install vllm" },
        {
          id: "serve",
          cmd: `vllm serve Qwen/Qwen3-8B \\
  --host 0.0.0.0 --port 8000 \\
  --enable-auto-tool-choice --tool-call-parser hermes`,
        },
        { id: "check", cmd: "curl http://192.168.1.50:8000/v1/models" },
      ],
    },
    auto: {
      baseUrl: "http://192.168.1.50:4000/v1",
      basic: "fast",
      advanced: "smart",
      steps: [
        { id: "oneModelEach" },
        {
          id: "twoServers",
          cmd: `vllm serve Qwen/Qwen3-4B --port 8001 \\
  --enable-auto-tool-choice --tool-call-parser hermes

vllm serve Qwen/Qwen3-14B --port 8002 \\
  --enable-auto-tool-choice --tool-call-parser hermes`,
        },
        {
          id: "proxy",
          cmd: `# litellm.yaml — one address in front of both servers
model_list:
  - model_name: fast
    litellm_params:
      model: openai/Qwen/Qwen3-4B
      api_base: http://127.0.0.1:8001/v1
      api_key: none
  - model_name: smart
    litellm_params:
      model: openai/Qwen/Qwen3-14B
      api_base: http://127.0.0.1:8002/v1
      api_key: none`,
        },
        {
          id: "run",
          cmd: "pip install 'litellm[proxy]'\nlitellm --config litellm.yaml --host 0.0.0.0 --port 4000",
        },
      ],
    },
  },

  localai: {
    single: {
      baseUrl: "http://192.168.1.50:8080/v1",
      model: "qwen3-8b",
      steps: [
        {
          id: "run",
          cmd: "docker run -d -p 8080:8080 --name local-ai \\\n  -v localai-models:/build/models localai/localai:latest",
        },
        { id: "install" },
        { id: "check", cmd: "curl http://192.168.1.50:8080/v1/models" },
      ],
    },
    auto: {
      baseUrl: "http://192.168.1.50:8080/v1",
      basic: "qwen3-4b",
      advanced: "qwen3-14b",
      steps: [
        { id: "installTwo" },
        { id: "sameEndpoint" },
        { id: "check", cmd: "curl http://192.168.1.50:8080/v1/models" },
      ],
    },
  },

  custom: {
    single: {
      baseUrl: "http://192.168.1.50:8000/v1",
      model: "my-model",
      steps: [
        { id: "requirements" },
        { id: "check", cmd: "curl http://192.168.1.50:8000/v1/models" },
      ],
    },
    auto: {
      baseUrl: "http://192.168.1.50:8000/v1",
      basic: "my-fast-model",
      advanced: "my-smart-model",
      steps: [
        { id: "twoNames" },
        { id: "check", cmd: "curl http://192.168.1.50:8000/v1/models" },
      ],
    },
  },
};

export function hasLocalGuide(provider: string): boolean {
  return provider in GUIDES;
}

export type GuideApply = {
  baseUrl: string;
  model: string;
  autoBasic?: string;
  autoAdvanced?: string;
};

export function LocalSetupGuide({
  provider,
  onApply,
}: {
  provider: string;
  onApply: (values: GuideApply) => void;
}) {
  const { t } = useTranslation();
  const guide = GUIDES[provider];
  // Someone who has already filled the endpoint in does not need the whole
  // thing unfolded over their settings; someone who has not almost certainly
  // does. Still a plain toggle either way.
  const [open, setOpen] = useState(true);
  const [mode, setMode] = useState<"single" | "auto">("single");
  if (!guide) return null;

  const plan = mode === "single" ? guide.single : guide.auto;
  const key = (step: string, part: string) =>
    `assistant.guide.${provider}.${mode}.${step}.${part}`;

  const apply = () =>
    onApply(
      mode === "single"
        ? { baseUrl: guide.single.baseUrl, model: guide.single.model }
        : {
            baseUrl: guide.auto.baseUrl,
            // Whatever does not go through Auto — the Telegram bridge, most
            // of all — answers on the plain model field, so it gets the
            // capable one rather than being left on a stale default.
            model: guide.auto.advanced,
            autoBasic: guide.auto.basic,
            autoAdvanced: guide.auto.advanced,
          },
    );

  return (
    <div className="border-edge overflow-hidden rounded-lg border">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className="bg-bg-subtle hover:bg-surface-hover flex w-full cursor-pointer items-center gap-2 px-3 py-2.5 text-left transition-colors"
      >
        <Wand2 className="text-accent size-3.5 shrink-0" />
        <span className="text-fg flex-1 text-[12.5px] font-medium">
          {t("assistant.guide.title")}
        </span>
        <ChevronDown className={cn("text-fg-subtle size-4 transition-transform", open && "rotate-180")} />
      </button>

      {open && (
        <div className="space-y-3 px-3 py-3">
          <div>
            <div className="border-edge inline-flex rounded-lg border p-0.5">
              {(["single", "auto"] as const).map((m) => (
                <button
                  key={m}
                  type="button"
                  onClick={() => setMode(m)}
                  className={cn(
                    "cursor-pointer rounded-md px-2.5 py-1 text-[12px] font-medium transition-colors",
                    mode === m ? "bg-accent text-accent-fg" : "text-fg-muted hover:text-fg",
                  )}
                >
                  {t(`assistant.guide.mode.${m}`)}
                </button>
              ))}
            </div>
            <p className="text-fg-muted mt-1.5 text-[12px] leading-relaxed">
              {t(`assistant.guide.mode.${mode}Hint`)}
            </p>
          </div>

          <ol className="space-y-3">
            {plan.steps.map((step, i) => (
              <GuideStep
                key={step.id}
                n={i + 1}
                title={t(key(step.id, "title"))}
                body={t(key(step.id, "body"))}
                cmd={step.cmd}
              />
            ))}

            {/* The last step is the same on every runtime: put the values in
                the fields above. It fills them in rather than asking the
                reader to retype an address they just read. */}
            <GuideStep
              n={plan.steps.length + 1}
              title={t("assistant.guide.hub.title")}
              body={
                mode === "single"
                  ? t("assistant.guide.hub.bodySingle", {
                      baseUrl: guide.single.baseUrl,
                      model: guide.single.model,
                    })
                  : t("assistant.guide.hub.bodyAuto", {
                      baseUrl: guide.auto.baseUrl,
                      basic: guide.auto.basic,
                      advanced: guide.auto.advanced,
                    })
              }
            >
              <Button size="sm" variant="secondary" onClick={apply}>
                <Wand2 className="size-3.5" />
                {t("assistant.guide.fillIn")}
              </Button>
            </GuideStep>
          </ol>

          <div className="text-fg-muted space-y-1.5 border-t border-[var(--hh-edge)] pt-2.5 text-[12px] leading-relaxed">
            <p>{t("assistant.guide.note.reachable")}</p>
            <p>{t("assistant.guide.note.tools")}</p>
            {mode === "auto" && <p>{t("assistant.guide.note.oneEndpoint")}</p>}
          </div>
        </div>
      )}
    </div>
  );
}

function GuideStep({
  n,
  title,
  body,
  cmd,
  children,
}: {
  n: number;
  title: string;
  body: string;
  cmd?: string;
  children?: React.ReactNode;
}) {
  return (
    <li className="flex gap-2.5">
      <span className="bg-accent-soft text-accent-soft-fg mt-0.5 flex size-5 shrink-0 items-center justify-center rounded-full text-[11px] font-semibold">
        {n}
      </span>
      <div className="min-w-0 flex-1 space-y-1.5">
        <p className="text-fg text-[12.5px] font-medium">{title}</p>
        <p className="text-fg-muted text-[12px] leading-relaxed">{body}</p>
        {cmd && <CommandBlock cmd={cmd} />}
        {children}
      </div>
    </li>
  );
}

/** A command to run, with the copy button that stops it being retyped. */
function CommandBlock({ cmd }: { cmd: string }) {
  const { t } = useTranslation();
  const [done, setDone] = useState(false);
  return (
    <div className="group relative">
      {/* Long flags and YAML both live here, so it scrolls rather than wraps
          into something that no longer looks like the thing you paste. */}
      <pre className="bg-bg border-edge text-fg-muted overflow-x-auto rounded-md border px-2.5 py-2 pr-9 font-mono text-[11.5px] leading-relaxed whitespace-pre">
        {cmd}
      </pre>
      <button
        type="button"
        aria-label={t("common.copy")}
        onClick={async () => {
          if (await copyText(cmd)) {
            setDone(true);
            setTimeout(() => setDone(false), 1200);
          }
        }}
        className="interactive-muted absolute top-1.5 right-1.5 flex size-6 cursor-pointer items-center justify-center"
      >
        {done ? <Check className="text-success size-3.5" /> : <Copy className="size-3.5" />}
      </button>
    </div>
  );
}
