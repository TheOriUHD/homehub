import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { AlertTriangle, Plus, Power, Radio, Server, Trash2, Zap } from "lucide-react";
import { api, isApiError, unwrap, type ApiSchemas } from "@/lib/api";
import { cn } from "@/lib/utils";
import { Badge } from "@/ui/badge";
import { Button } from "@/ui/button";
import { Field } from "@/ui/field";
import { Input } from "@/ui/input";
import { PageLoader } from "@/ui/misc";
import { EmptyState } from "@/ui/states";
import { useToast } from "@/ui/toast";
import { CopyIconButton, ToolCard } from "../kit";

/**
 * Wake-on-LAN. Two ways to wake a machine: from a saved target (a named
 * MAC + broadcast/port kept server-side) or a one-off "quick wake" that
 * fires a magic packet without saving anything. MAC format is validated in
 * the browser for instant feedback; the server validates again and is the
 * authority — its 400s surface inline just like the local checks.
 */

type WolTarget = ApiSchemas["WolTarget"];

const DEFAULT_BROADCAST = "255.255.255.255";
const DEFAULT_PORT = "9";

// Six hex pairs joined by ":" or "-" (mixed separators are rejected below).
const MAC_RE = /^([0-9a-fA-F]{2}([:-])){5}[0-9a-fA-F]{2}$/;

function macError(raw: string): string | null {
  const mac = raw.trim();
  if (!mac) return "Enter a MAC address.";
  if (!MAC_RE.test(mac)) return "Not a valid MAC — use 6 hex pairs, e.g. AA:BB:CC:DD:EE:FF.";
  // Guard against a mixed "AA:BB-CC" that the alternation above would allow.
  if (mac.includes(":") && mac.includes("-")) return "Use one separator throughout — all “:” or all “-”.";
  return null;
}

/** Validate an optional port; empty means "use the default". */
function portError(raw: string): string | null {
  const v = raw.trim();
  if (!v) return null;
  if (!/^\d+$/.test(v)) return "Port must be a whole number.";
  const n = Number(v);
  if (n < 1 || n > 65535) return "Port must be between 1 and 65535.";
  return null;
}

const msgOf = (e: unknown) => (isApiError(e) ? e.message : String(e));

interface WakeVars {
  key: string; // "quick" for the one-off form, otherwise the target id
  label: string;
  body: { targetId?: string; mac?: string; broadcast?: string; port?: number };
}

// A small inline error banner, used wherever an action can fail.
function InlineError({ message }: { message: string }) {
  return (
    <div
      role="alert"
      className="border-danger/40 bg-danger-soft/40 text-danger mt-3 flex items-start gap-2 rounded-lg border px-3 py-2 text-[13px]"
    >
      <AlertTriangle className="mt-0.5 size-4 shrink-0" />
      <span className="min-w-0">{message}</span>
    </div>
  );
}

export default function Tool() {
  const toast = useToast();
  const qc = useQueryClient();

  const targets = useQuery({
    queryKey: ["wol", "targets"],
    queryFn: async () => unwrap(await api.GET("/tools/wol/targets")),
  });

  const invalidate = () => void qc.invalidateQueries({ queryKey: ["wol"] });

  // ---- shared wake mutation (saved targets + quick wake) -------------------
  const [listError, setListError] = useState<string | null>(null);
  const [quickError, setQuickError] = useState<string | null>(null);

  const wake = useMutation({
    mutationFn: async (v: WakeVars) => unwrap(await api.POST("/tools/wol/wake", { body: v.body })),
    onMutate: (v) => {
      if (v.key === "quick") setQuickError(null);
      else setListError(null);
    },
    onSuccess: (_res, v) => toast("success", `Magic packet sent to ${v.label}`),
    onError: (err, v) => {
      const m = msgOf(err);
      if (v.key === "quick") setQuickError(m);
      else setListError(m);
      toast("error", m);
    },
  });
  const isWaking = (key: string) => wake.isPending && wake.variables?.key === key;

  // ---- delete a saved target ----------------------------------------------
  const remove = useMutation({
    mutationFn: async (id: string) => unwrap(await api.DELETE("/tools/wol/targets/{id}", { params: { path: { id } } })),
    onSuccess: () => invalidate(),
    onError: (err) => {
      const m = msgOf(err);
      setListError(m);
      toast("error", m);
    },
  });

  // ---- add-target form -----------------------------------------------------
  const [name, setName] = useState("");
  const [mac, setMac] = useState("");
  const [broadcast, setBroadcast] = useState("");
  const [port, setPort] = useState("");
  const [addTouched, setAddTouched] = useState(false);
  const [addError, setAddError] = useState<string | null>(null);

  const addNameErr = name.trim() ? null : "Give the device a name.";
  const addMacErr = macError(mac);
  const addPortErr = portError(port);
  const addValid = !addNameErr && !addMacErr && !addPortErr;

  const create = useMutation({
    mutationFn: async () => {
      const body: { name: string; mac: string; broadcast?: string; port?: number } = {
        name: name.trim(),
        mac: mac.trim(),
      };
      if (broadcast.trim()) body.broadcast = broadcast.trim();
      if (port.trim()) body.port = Number(port.trim());
      return unwrap(await api.POST("/tools/wol/targets", { body }));
    },
    onSuccess: (created) => {
      toast("success", `Saved ${created.name}`);
      setName("");
      setMac("");
      setBroadcast("");
      setPort("");
      setAddTouched(false);
      setAddError(null);
      invalidate();
    },
    onError: (err) => {
      const m = msgOf(err);
      setAddError(m);
      toast("error", m);
    },
  });

  const submitAdd = () => {
    setAddTouched(true);
    setAddError(null);
    if (!addValid) return;
    create.mutate();
  };

  // ---- quick-wake form -----------------------------------------------------
  const [qMac, setQMac] = useState("");
  const [qBroadcast, setQBroadcast] = useState("");
  const [qPort, setQPort] = useState("");
  const [qTouched, setQTouched] = useState(false);

  const qMacErr = macError(qMac);
  const qPortErr = portError(qPort);

  const submitQuick = () => {
    setQTouched(true);
    setQuickError(null);
    if (qMacErr || qPortErr) return;
    const body: WakeVars["body"] = { mac: qMac.trim() };
    if (qBroadcast.trim()) body.broadcast = qBroadcast.trim();
    if (qPort.trim()) body.port = Number(qPort.trim());
    wake.mutate({ key: "quick", label: qMac.trim().toUpperCase(), body });
  };

  const list = targets.data ?? [];

  return (
    <div className="space-y-4">
      {/* -------- Saved targets -------- */}
      <ToolCard
        title={
          <span className="flex items-center gap-2">
            <Server className="text-fg-muted size-4" />
            Saved devices
          </span>
        }
        right={list.length > 0 ? <Badge tone="neutral">{list.length}</Badge> : undefined}
      >
        {targets.isLoading ? (
          <PageLoader className="py-10" />
        ) : targets.isError ? (
          <InlineError message={msgOf(targets.error)} />
        ) : list.length === 0 ? (
          <EmptyState
            icon={Radio}
            title="No saved devices yet"
            body="Add a device below to keep its MAC handy, then wake it with one click."
          />
        ) : (
          <div className="border-edge divide-edge divide-y overflow-hidden rounded-lg border">
            {list.map((tgt) => (
              <TargetRow
                key={tgt.id}
                target={tgt}
                waking={isWaking(tgt.id)}
                deleting={remove.isPending && remove.variables === tgt.id}
                onWake={() =>
                  wake.mutate({ key: tgt.id, label: tgt.name, body: { targetId: tgt.id } })
                }
                onDelete={() => remove.mutate(tgt.id)}
              />
            ))}
          </div>
        )}
        {listError && <InlineError message={listError} />}
      </ToolCard>

      {/* -------- Add a device -------- */}
      <ToolCard
        title={
          <span className="flex items-center gap-2">
            <Plus className="text-fg-muted size-4" />
            Add a device
          </span>
        }
      >
        <form
          onSubmit={(e) => {
            e.preventDefault();
            submitAdd();
          }}
          className="grid gap-3 sm:grid-cols-2"
        >
          <div className="sm:col-span-2">
            <Field label="Device name" error={addTouched ? addNameErr : null}>
              {(p) => (
                <Input
                  {...p}
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  placeholder="NAS · gaming-pc · living-room-tv"
                  autoComplete="off"
                />
              )}
            </Field>
          </div>
          <div className="sm:col-span-2">
            <Field
              label="MAC address"
              error={addTouched ? addMacErr : null}
              hint={addTouched && addMacErr ? undefined : "AA:BB:CC:DD:EE:FF or AA-BB-CC-DD-EE-FF"}
            >
              {(p) => (
                <Input
                  {...p}
                  value={mac}
                  onChange={(e) => setMac(e.target.value)}
                  placeholder="AA:BB:CC:DD:EE:FF"
                  spellCheck={false}
                  autoComplete="off"
                  autoCapitalize="characters"
                  className="font-mono"
                />
              )}
            </Field>
          </div>
          <Field label="Broadcast" optional="optional">
            {(p) => (
              <Input
                {...p}
                value={broadcast}
                onChange={(e) => setBroadcast(e.target.value)}
                placeholder={DEFAULT_BROADCAST}
                spellCheck={false}
                autoComplete="off"
                className="font-mono"
              />
            )}
          </Field>
          <Field label="Port" optional="optional" error={addTouched ? addPortErr : null}>
            {(p) => (
              <Input
                {...p}
                value={port}
                onChange={(e) => setPort(e.target.value)}
                placeholder={DEFAULT_PORT}
                inputMode="numeric"
                autoComplete="off"
                className="font-mono"
              />
            )}
          </Field>
          <div className="flex justify-end sm:col-span-2">
            <Button type="submit" variant="primary" size="sm" loading={create.isPending}>
              <Plus className="size-3.5" />
              Add device
            </Button>
          </div>
        </form>
        {addError && <InlineError message={addError} />}
      </ToolCard>

      {/* -------- Quick wake -------- */}
      <ToolCard
        title={
          <span className="flex items-center gap-2">
            <Zap className="text-fg-muted size-4" />
            Quick wake
          </span>
        }
      >
        <p className="text-fg-muted mb-3 text-[13px]">
          Fire a magic packet to any MAC without saving it. Broadcast defaults to{" "}
          <span className="font-mono">{DEFAULT_BROADCAST}</span> and port to{" "}
          <span className="font-mono">{DEFAULT_PORT}</span>.
        </p>
        <form
          onSubmit={(e) => {
            e.preventDefault();
            submitQuick();
          }}
          className="flex flex-col gap-3 sm:flex-row sm:items-start"
        >
          <div className="min-w-0 flex-1">
            <Field
              label="MAC address"
              error={qTouched ? qMacErr : null}
              hint={qTouched && qMacErr ? undefined : "6 hex pairs"}
            >
              {(p) => (
                <Input
                  {...p}
                  value={qMac}
                  onChange={(e) => setQMac(e.target.value)}
                  placeholder="AA:BB:CC:DD:EE:FF"
                  spellCheck={false}
                  autoComplete="off"
                  autoCapitalize="characters"
                  className="font-mono"
                />
              )}
            </Field>
          </div>
          <div className="sm:w-40">
            <Field label="Broadcast" optional="optional">
              {(p) => (
                <Input
                  {...p}
                  value={qBroadcast}
                  onChange={(e) => setQBroadcast(e.target.value)}
                  placeholder={DEFAULT_BROADCAST}
                  spellCheck={false}
                  autoComplete="off"
                  className="font-mono"
                />
              )}
            </Field>
          </div>
          <div className="sm:w-24">
            <Field label="Port" optional="optional" error={qTouched ? qPortErr : null}>
              {(p) => (
                <Input
                  {...p}
                  value={qPort}
                  onChange={(e) => setQPort(e.target.value)}
                  placeholder={DEFAULT_PORT}
                  inputMode="numeric"
                  autoComplete="off"
                  className="font-mono"
                />
              )}
            </Field>
          </div>
          <div className="sm:pt-[26px]">
            <Button
              type="submit"
              variant="primary"
              size="sm"
              loading={isWaking("quick")}
              className="w-full sm:w-auto"
            >
              <Power className="size-3.5" />
              Wake now
            </Button>
          </div>
        </form>
        {quickError && <InlineError message={quickError} />}
      </ToolCard>
    </div>
  );
}

// --------------------------------------------------------------------------

function TargetRow({
  target,
  waking,
  deleting,
  onWake,
  onDelete,
}: {
  target: WolTarget;
  waking: boolean;
  deleting: boolean;
  onWake: () => void;
  onDelete: () => void;
}) {
  return (
    <div className="group hover:bg-bg-subtle/60 flex items-center gap-3 px-3 py-2.5 transition-colors">
      <span className="bg-accent-soft text-accent-soft-fg flex size-9 shrink-0 items-center justify-center rounded-lg">
        <Server className="size-4" />
      </span>
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-1">
          <span className="text-fg truncate text-sm font-medium">{target.name}</span>
          <CopyIconButton value={target.mac} label="Copy MAC" />
        </div>
        <p className="text-fg-subtle truncate font-mono text-[12px]">
          {target.mac}
          <span className="text-fg-subtle/70">
            {" · "}
            {target.broadcast}:{target.port}
          </span>
        </p>
      </div>
      <Button size="sm" variant="primary" loading={waking} onClick={onWake}>
        <Power className="size-3.5" />
        Wake
      </Button>
      <button
        type="button"
        onClick={onDelete}
        disabled={deleting}
        aria-label={`Delete ${target.name}`}
        title="Delete"
        className={cn(
          "text-fg-subtle hover:text-danger hover:bg-bg-subtle rounded-md p-1.5 transition-colors",
          "disabled:pointer-events-none disabled:opacity-50",
        )}
      >
        <Trash2 className="size-4" />
      </button>
    </div>
  );
}
