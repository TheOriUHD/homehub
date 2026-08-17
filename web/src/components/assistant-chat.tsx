import { useEffect, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import {
  Box,
  Eraser,
  Gamepad2,
  Lightbulb,
  Pickaxe,
  Send,
  Server,
  ShieldCheck,
  Sparkles,
  Wrench,
} from "lucide-react";
import { api, isApiError, unwrap, type HaEntity } from "@/lib/api";
import { cn } from "@/lib/utils";
import { Badge, StatusDot } from "@/ui/badge";
import { Button } from "@/ui/button";
import { ConfirmDialog } from "@/ui/confirm";
import { Input } from "@/ui/input";
import { PageLoader } from "@/ui/misc";
import { Popover, PopoverContent, PopoverTrigger } from "@/ui/popover";
import { Slider } from "@/ui/slider";
import { Switch } from "@/ui/switch";
import { useToast } from "@/ui/toast";

/**
 * The shared assistant conversation — one rolling thread per user, shown
 * identically on the /assistant page and in the floating widget (both ride
 * the same query key, so they stay in sync). Assistant messages render
 * markdown, and every action the model took becomes a live chip: a light
 * it touched opens instant controls, a container it restarted shows its
 * lifecycle buttons. The chat is the receipt AND the remote.
 */

export type AssistantStep = {
  tool: string;
  input?: Record<string, unknown>;
  isError?: boolean;
};

type HistoryItem = {
  id: number;
  role: "user" | "assistant";
  content: string;
  tools?: string[];
  steps?: AssistantStep[];
  ts: string;
  pending?: boolean;
};

export function AssistantChatPane({ variant }: { variant: "page" | "widget" }) {
  const { t } = useTranslation();
  const toast = useToast();
  const queryClient = useQueryClient();
  const [draft, setDraft] = useState("");
  const [confirmClear, setConfirmClear] = useState(false);
  const bottomRef = useRef<HTMLDivElement>(null);

  const history = useQuery({
    queryKey: ["assistant", "history"],
    queryFn: async () => unwrap(await api.GET("/assistant/history")),
  });

  const send = useMutation({
    mutationFn: async (message: string) =>
      unwrap(await api.POST("/assistant/chat", { body: { message } })),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ["assistant", "history"] }),
    onError: (err) => {
      void queryClient.invalidateQueries({ queryKey: ["assistant", "history"] });
      toast("error", t("assistant.failed"), isApiError(err) ? err.message : t("common.unknownError"));
    },
  });

  const clear = useMutation({
    mutationFn: async () => {
      unwrap(await api.DELETE("/assistant/history"));
    },
    onSuccess: () => {
      setConfirmClear(false);
      void queryClient.invalidateQueries({ queryKey: ["assistant", "history"] });
    },
  });

  const items: HistoryItem[] = [...((history.data as HistoryItem[] | undefined) ?? [])];
  if (send.isPending && send.variables) {
    items.push({ id: -1, role: "user", content: send.variables, ts: "" });
    items.push({ id: -2, role: "assistant", content: "", ts: "", pending: true });
  }

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: "smooth", block: "end" });
  }, [items.length, send.isPending]);

  const submit = () => {
    const message = draft.trim();
    if (!message || send.isPending) return;
    setDraft("");
    send.mutate(message);
  };

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-3">
      <div
        className={cn(
          "flex-1 space-y-3 overflow-y-auto pr-1",
          variant === "page" ? "min-h-[300px]" : "min-h-0",
        )}
      >
        {history.isLoading && <PageLoader className="py-10" />}
        {!history.isLoading && items.length === 0 && (
          <div className="text-fg-muted flex h-full flex-col items-center justify-center gap-1 py-10 text-center text-sm">
            <Sparkles className="text-fg-subtle mb-1 size-5" />
            {t("assistant.emptyHint")}
            <span className="text-fg-subtle text-[12.5px]">{t("assistant.emptyExamples")}</span>
          </div>
        )}
        {items.map((item) => (
          <MessageBubble key={item.id} item={item} compact={variant === "widget"} />
        ))}
        <div ref={bottomRef} />
      </div>

      <div className="flex items-end gap-2">
        <Input
          value={draft}
          placeholder={t("assistant.placeholder")}
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && !e.shiftKey) {
              e.preventDefault();
              submit();
            }
          }}
        />
        <Button
          variant="primary"
          disabled={!draft.trim() || send.isPending}
          onClick={submit}
          aria-label={t("assistant.send")}
        >
          <Send className="size-4" />
        </Button>
        {variant === "page" && items.length > 0 && (
          <Button size="sm" variant="ghost" onClick={() => setConfirmClear(true)} aria-label={t("assistant.clear")}>
            <Eraser className="size-4" />
          </Button>
        )}
      </div>

      <ConfirmDialog
        open={confirmClear}
        onOpenChange={setConfirmClear}
        title={t("assistant.clearTitle")}
        body={t("assistant.clearBody")}
        confirmLabel={t("assistant.clear")}
        loading={clear.isPending}
        onConfirm={() => clear.mutate()}
      />
    </div>
  );
}

/* ---- message rendering ------------------------------------------------------ */

function MessageBubble({ item, compact }: { item: HistoryItem; compact?: boolean }) {
  const { t } = useTranslation();
  const isUser = item.role === "user";
  return (
    <div className={cn("flex", isUser ? "justify-end" : "justify-start")}>
      <div
        className={cn(
          "max-w-[85%] rounded-xl px-3.5 py-2.5 text-sm",
          isUser ? "bg-accent text-accent-fg" : "bg-bg-subtle text-fg",
        )}
      >
        {item.pending ? (
          <span className="text-fg-muted flex items-center gap-2 text-[13px]">
            <Sparkles className="size-3.5 animate-pulse" />
            {t("assistant.thinking")}
          </span>
        ) : isUser ? (
          <p className="whitespace-pre-wrap">{item.content}</p>
        ) : (
          <AssistantMarkdown content={item.content} />
        )}
        {!isUser && !item.pending && <ActionChips steps={item.steps ?? []} compact={compact} />}
      </div>
    </div>
  );
}

/** Markdown scoped to chat: bold, italics, code, lists, links, tables —
 * styled with design tokens, no raw HTML ever. */
function AssistantMarkdown({ content }: { content: string }) {
  return (
    <div className="space-y-2 [&>*:first-child]:mt-0 [&>*:last-child]:mb-0">
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        components={{
          p: (props) => <p className="leading-relaxed whitespace-pre-wrap" {...props} />,
          strong: (props) => <strong className="font-semibold" {...props} />,
          em: (props) => <em className="italic" {...props} />,
          a: ({ href, children }) => (
            <a
              href={href}
              target="_blank"
              rel="noopener noreferrer"
              className="text-accent underline underline-offset-2"
            >
              {children}
            </a>
          ),
          ul: (props) => <ul className="ml-4 list-disc space-y-1" {...props} />,
          ol: (props) => <ol className="ml-4 list-decimal space-y-1" {...props} />,
          li: (props) => <li className="leading-relaxed" {...props} />,
          code: ({ className, children }) => {
            const block = /language-/.test(className ?? "");
            return block ? (
              <code className="block font-mono text-[12.5px]">{children}</code>
            ) : (
              <code className="bg-bg rounded px-1 py-0.5 font-mono text-[12.5px]">{children}</code>
            );
          },
          pre: (props) => (
            <pre
              className="bg-bg border-edge overflow-x-auto rounded-md border px-3 py-2 text-[12.5px]"
              {...props}
            />
          ),
          h1: (props) => <p className="text-[15px] font-semibold" {...props} />,
          h2: (props) => <p className="text-[14px] font-semibold" {...props} />,
          h3: (props) => <p className="font-semibold" {...props} />,
          table: (props) => (
            <div className="overflow-x-auto">
              <table className="border-edge w-full border-collapse text-[12.5px] [&_td]:border [&_td]:border-(--hh-edge) [&_td]:px-2 [&_td]:py-1 [&_th]:border [&_th]:border-(--hh-edge) [&_th]:px-2 [&_th]:py-1 [&_th]:text-left [&_th]:font-semibold" {...props} />
            </div>
          ),
          blockquote: (props) => (
            <blockquote className="border-edge-strong text-fg-muted border-l-2 pl-3" {...props} />
          ),
        }}
      >
        {content}
      </ReactMarkdown>
    </div>
  );
}

/* ---- action chips ----------------------------------------------------------- */

/**
 * Chips are the receipt of what the model DID, and live handles on it:
 * touched Home Assistant entities open instant controls, containers show
 * lifecycle buttons, everything else links into its module. Read tools
 * stay as quiet wrench tags.
 */
function ActionChips({ steps, compact }: { steps: AssistantStep[]; compact?: boolean }) {
  const { t } = useTranslation();
  if (steps.length === 0) return null;

  const str = (step: AssistantStep, key: string) =>
    typeof step.input?.[key] === "string" ? (step.input[key] as string) : "";

  const ok = steps.filter((s) => !s.isError);
  const haSteps = ok.filter((s) => s.tool === "ha_call_service" && str(s, "entityId"));
  // One chip per distinct entity; one grouped chip when several were touched.
  const haEntities = [...new Map(haSteps.map((s) => [str(s, "systemId") + "|" + str(s, "entityId"), s])).values()];
  const dockerSteps = [...new Map(
    ok.filter((s) => s.tool === "docker_container_action" && str(s, "name"))
      .map((s) => [str(s, "systemId") + "|" + str(s, "name"), s]),
  ).values()];
  const linkSteps = ok.filter((s) =>
    ["mc_command", "amp_instance_action", "pve_guest_action", "dns_blocking"].includes(s.tool),
  );
  const readTools = [...new Set(steps.filter((s) =>
    !["ha_call_service", "docker_container_action", "mc_command", "amp_instance_action", "pve_guest_action", "dns_blocking"].includes(s.tool),
  ).map((s) => s.tool))];

  const hasAction = haEntities.length + dockerSteps.length + linkSteps.length > 0;
  if (!hasAction && readTools.length === 0) return null;

  return (
    <div className="mt-2 flex flex-wrap items-center gap-1.5">
      {haEntities.length === 1 && (
        <HaEntityChip systemId={str(haEntities[0], "systemId")} entityId={str(haEntities[0], "entityId")} />
      )}
      {haEntities.length > 1 && (
        <HaGroupChip
          entities={haEntities.map((s) => ({ systemId: str(s, "systemId"), entityId: str(s, "entityId") }))}
        />
      )}
      {dockerSteps.map((s) => (
        <DockerChip key={str(s, "name")} systemId={str(s, "systemId")} name={str(s, "name")} />
      ))}
      {linkSteps.map((s, i) => (
        <LinkChip key={i} step={s} />
      ))}
      {readTools.map((tool) => (
        <Badge key={tool} tone="neutral">
          <Wrench className="size-2.5" />
          {tool}
        </Badge>
      ))}
      {!compact && hasAction && (
        <span className="text-fg-subtle text-[10.5px]">{t("assistant.chips.tapHint")}</span>
      )}
    </div>
  );
}

const chipClass =
  "border-edge bg-surface hover:border-accent/60 hover:bg-surface-hover flex cursor-pointer items-center gap-1.5 rounded-full border px-2.5 py-1 text-[12px] font-medium shadow-1 transition-colors";

/** One touched Home Assistant entity: live state dot + instant controls. */
function HaEntityChip({ systemId, entityId }: { systemId: string; entityId: string }) {
  const [open, setOpen] = useState(false);
  const entity = useHaEntity(systemId, entityId, open);
  const on = entity.data?.state === "on";
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <button type="button" className={chipClass}>
          <Lightbulb className={cn("size-3", on ? "text-warning" : "text-fg-subtle")} />
          <span className="text-fg">{entity.data?.name ?? entityId.split(".")[1]}</span>
          <StatusDot tone={on ? "success" : "neutral"} />
        </button>
      </PopoverTrigger>
      <PopoverContent align="start" className="w-64">
        <HaEntityControls systemId={systemId} entityId={entityId} />
      </PopoverContent>
    </Popover>
  );
}

/** Several entities in one turn: a grouped chip that expands to a rail of
 * individually controllable rows. */
function HaGroupChip({ entities }: { entities: Array<{ systemId: string; entityId: string }> }) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <button type="button" className={chipClass}>
          <Lightbulb className="text-warning size-3" />
          <span className="text-fg">{t("assistant.chips.devices", { count: entities.length })}</span>
        </button>
      </PopoverTrigger>
      <PopoverContent align="start" className="max-h-80 w-72 space-y-3 overflow-y-auto">
        {entities.map((e) => (
          <HaEntityControls key={e.entityId} systemId={e.systemId} entityId={e.entityId} />
        ))}
      </PopoverContent>
    </Popover>
  );
}

function useHaEntity(systemId: string, entityId: string, live: boolean) {
  return useQuery({
    queryKey: ["ha", systemId, "entity", entityId],
    queryFn: async () =>
      unwrap(
        await api.GET("/ha/{id}/entities/{entityId}", {
          params: { path: { id: systemId, entityId } },
        }),
      ),
    refetchInterval: live ? 4_000 : false,
  });
}

/** Instant control row: name + state, toggle, brightness when it's a lit
 * light. The same service calls the HA boards make. */
function HaEntityControls({ systemId, entityId }: { systemId: string; entityId: string }) {
  const { t } = useTranslation();
  const toast = useToast();
  const queryClient = useQueryClient();
  const entity = useHaEntity(systemId, entityId, true);

  const callService = useMutation({
    mutationFn: async ({ service, data }: { service: string; data?: Record<string, unknown> }) => {
      unwrap(
        await api.POST("/ha/{id}/entities/{entityId}/service", {
          params: { path: { id: systemId, entityId } },
          body: data ? { service, data } : { service },
        }),
      );
    },
    onSuccess: () =>
      void queryClient.invalidateQueries({ queryKey: ["ha", systemId, "entity", entityId] }),
    onError: (err) =>
      toast("error", t("assistant.chips.controlFailed"), isApiError(err) ? err.message : undefined),
  });

  const e = entity.data as (HaEntity & { attributes?: Record<string, unknown> }) | undefined;
  if (!e) return <PageLoader className="py-4" />;

  const domain = entityId.split(".")[0];
  const toggleable = ["light", "switch", "fan", "input_boolean", "media_player"].includes(domain);
  const on = e.state === "on";
  const brightness = typeof e.attributes?.brightness === "number" ? (e.attributes.brightness as number) : null;

  return (
    <div className="space-y-2.5">
      <div className="flex items-center justify-between gap-3">
        <div className="min-w-0">
          <p className="text-fg truncate text-[13px] font-medium">{e.name}</p>
          <p className="text-fg-subtle truncate text-[11.5px]">
            {entityId} · {e.state}
          </p>
        </div>
        {toggleable ? (
          <Switch
            checked={on}
            disabled={callService.isPending}
            onCheckedChange={() => callService.mutate({ service: "toggle" })}
            aria-label={t("assistant.chips.toggle", { name: e.name })}
          />
        ) : (
          <Badge tone={on ? "success" : "neutral"}>{e.state}</Badge>
        )}
      </div>
      {domain === "light" && on && brightness !== null && (
        <Slider
          value={Math.round((brightness / 255) * 100)}
          min={1}
          max={100}
          onValueCommit={(v) => callService.mutate({ service: "turn_on", data: { brightness_pct: v } })}
          aria-label={t("assistant.chips.brightness")}
        />
      )}
    </div>
  );
}

/** A container the model touched: live state + lifecycle buttons. */
function DockerChip({ systemId, name }: { systemId: string; name: string }) {
  const { t } = useTranslation();
  const toast = useToast();
  const queryClient = useQueryClient();
  const [open, setOpen] = useState(false);

  const container = useQuery({
    queryKey: ["docker", systemId, "container", name],
    queryFn: async () =>
      unwrap(
        await api.GET("/docker/{id}/containers/{name}", {
          params: { path: { id: systemId, name } },
        }),
      ),
    enabled: open,
    refetchInterval: open ? 4_000 : false,
  });

  const action = useMutation({
    mutationFn: async (act: "start" | "stop" | "restart") => {
      unwrap(
        await api.POST("/docker/{id}/containers/{name}/action", {
          params: { path: { id: systemId, name } },
          body: { action: act },
        }),
      );
    },
    onSuccess: () =>
      void queryClient.invalidateQueries({ queryKey: ["docker", systemId, "container", name] }),
    onError: (err) =>
      toast("error", t("assistant.chips.controlFailed"), isApiError(err) ? err.message : undefined),
  });

  const running = container.data?.container.state === "running";
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <button type="button" className={chipClass}>
          <Box className="text-fg-subtle size-3" />
          <span className="text-fg">{name}</span>
        </button>
      </PopoverTrigger>
      <PopoverContent align="start" className="w-60 space-y-2.5">
        <div className="flex items-center justify-between gap-2">
          <p className="text-fg truncate text-[13px] font-medium">{name}</p>
          <Badge tone={running ? "success" : "neutral"}>
            {container.data?.container.state ?? "…"}
          </Badge>
        </div>
        <div className="flex gap-1.5">
          {running ? (
            <>
              <Button size="sm" variant="secondary" loading={action.isPending} onClick={() => action.mutate("restart")}>
                {t("assistant.chips.restart")}
              </Button>
              <Button size="sm" variant="danger-ghost" loading={action.isPending} onClick={() => action.mutate("stop")}>
                {t("assistant.chips.stop")}
              </Button>
            </>
          ) : (
            <Button size="sm" variant="primary" loading={action.isPending} onClick={() => action.mutate("start")}>
              {t("assistant.chips.start")}
            </Button>
          )}
        </div>
      </PopoverContent>
    </Popover>
  );
}

/** Actions whose home is a module page: chip navigates there. */
function LinkChip({ step }: { step: AssistantStep }) {
  const systemId = typeof step.input?.systemId === "string" ? (step.input.systemId as string) : "";
  let to = "";
  let icon = Server;
  let label = step.tool;
  switch (step.tool) {
    case "mc_command":
      to = `/mc/${systemId}`;
      icon = Pickaxe;
      label = typeof step.input?.command === "string" ? `/${step.input.command as string}` : "console";
      break;
    case "amp_instance_action":
      to = `/amp/${systemId}`;
      icon = Gamepad2;
      label = `${step.input?.action ?? ""} ${step.input?.instanceId ?? ""}`.trim();
      break;
    case "pve_guest_action":
      to = `/pve/${systemId}`;
      icon = Server;
      label = `${step.input?.action ?? ""} #${step.input?.vmid ?? ""}`.trim();
      break;
    case "dns_blocking":
      to = "/dns";
      icon = ShieldCheck;
      label = step.input?.enabled === false ? "blocking off" : "blocking on";
      break;
  }
  const Icon = icon;
  if (!to) return null;
  return (
    <Link to={to} className={chipClass}>
      <Icon className="text-fg-subtle size-3" />
      <span className="text-fg">{label}</span>
    </Link>
  );
}
