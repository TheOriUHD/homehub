import { useEffect, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useParams } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { AlertTriangle, Cctv, Database, ExternalLink, Loader2, Video } from "lucide-react";
import { api, unwrap, type ApertureCamera } from "@/lib/api";
import { useIntegrations, statusTone } from "@/lib/systems";
import { cn, formatBytes } from "@/lib/utils";
import { Badge, StatusDot } from "@/ui/badge";
import { Card, CardBody } from "@/ui/card";
import { Dialog, DialogContent } from "@/ui/dialog";
import { PageLoader } from "@/ui/misc";
import { Sparkline } from "@/ui/sparkline";
import { EmptyState, ErrorState } from "@/ui/states";
import { acquireLive, releaseLive, type LiveState } from "./live-engine";

export function NvrModulePage() {
  const { t } = useTranslation();
  const { instanceId } = useParams({ from: "/shell/nvr/$instanceId" });
  const integrations = useIntegrations();

  const overview = useQuery({
    queryKey: ["nvr", instanceId, "overview"],
    queryFn: async () =>
      unwrap(await api.GET("/nvr/{id}/overview", { params: { path: { id: instanceId } } })),
    refetchInterval: 15_000,
  });

  const [expanded, setExpanded] = useState<ApertureCamera | null>(null);

  if (integrations.isLoading || overview.isLoading) return <PageLoader />;
  const instance = integrations.data?.find((row) => row.id === instanceId);
  if (!instance) {
    return (
      <Card>
        <EmptyState icon={AlertTriangle} title={t("errors.notFound.title")} body={t("errors.notFound.body")} />
      </Card>
    );
  }
  if (overview.isError) {
    return (
      <div className="space-y-5">
        <Header name={instance.name} status={instance.status} baseUrl={null} />
        <Card>
          <ErrorState error={overview.error} onRetry={() => void overview.refetch()} />
        </Card>
      </div>
    );
  }

  const data = overview.data!;
  const cameras = data.cameras;
  const recording = cameras.filter((c) => c.recordingOk).length;
  const retentionHours = cameras
    .filter((c) => c.oldestTs && c.newestTs && c.newestTs > c.oldestTs)
    .map((c) => (c.newestTs! - c.oldestTs!) / 3_600_000);
  const minRetention = retentionHours.length > 0 ? Math.min(...retentionHours) : null;
  const usedBytes = cameras.reduce((sum, c) => sum + (c.storageBytes ?? 0), 0);
  const bitratePoints = data.bitrate.map((b) => ({ ts: Math.floor(b.ts / 1000), value: b.bps }));
  const latestBps = data.bitrate.length > 0 ? data.bitrate[data.bitrate.length - 1].bps : null;

  return (
    <div className="space-y-5">
      <Header name={instance.name} status={instance.status} baseUrl={data.baseUrl} />

      <div className="grid items-stretch gap-3 md:grid-cols-3">
        <Card>
          <CardBody className="flex items-center gap-4">
            <span className="bg-accent-soft text-accent-soft-fg flex size-11 shrink-0 items-center justify-center rounded-xl">
              <Video className="size-5" />
            </span>
            <div>
              <p className="text-fg text-2xl font-semibold tracking-tight">
                {recording}
                <span className="text-fg-muted text-base font-normal"> / {cameras.length}</span>
              </p>
              <p className="text-fg-muted text-[13px]">{t("nvr.overview.recording")}</p>
            </div>
          </CardBody>
        </Card>
        <Card>
          <CardBody className="flex items-center gap-4">
            <span className="bg-accent-soft text-accent-soft-fg flex size-11 shrink-0 items-center justify-center rounded-xl">
              <Database className="size-5" />
            </span>
            <div className="min-w-0">
              <p className="text-fg text-2xl font-semibold tracking-tight">
                {minRetention !== null
                  ? minRetention >= 48
                    ? t("nvr.overview.days", { days: (minRetention / 24).toFixed(1) })
                    : t("nvr.overview.hours", { hours: Math.round(minRetention) })
                  : "—"}
              </p>
              <p className="text-fg-muted text-[13px]">
                {t("nvr.overview.retention")}
                {data.capBytes
                  ? ` · ${formatBytes(usedBytes)} / ${formatBytes(data.capBytes)}`
                  : ""}
              </p>
            </div>
          </CardBody>
        </Card>
        <Card>
          <CardBody className="flex items-center gap-4">
            <div className="min-w-0 flex-1">
              <p className="text-fg text-2xl font-semibold tracking-tight">
                {latestBps !== null ? `${(latestBps / 1_000_000).toFixed(1)} Mb/s` : "—"}
              </p>
              <p className="text-fg-muted text-[13px]">{t("nvr.overview.throughput")}</p>
            </div>
            {bitratePoints.length > 1 && (
              <div className="w-[130px] shrink-0">
                <Sparkline points={bitratePoints} height={40} />
              </div>
            )}
          </CardBody>
        </Card>
      </div>

      {cameras.length === 0 ? (
        <Card>
          <EmptyState icon={Cctv} title={t("nvr.grid.emptyTitle")} body={t("nvr.grid.emptyBody")} />
        </Card>
      ) : (
        <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-2">
          {cameras.map((cam) => (
            <CameraTile
              key={cam.id}
              instanceId={instanceId}
              camera={cam}
              quality="sub"
              onExpand={() => setExpanded(cam)}
            />
          ))}
        </div>
      )}

      {expanded && (
        <Dialog open onOpenChange={(open) => !open && setExpanded(null)}>
          <DialogContent wide className="max-w-[min(94vw,1700px)]" title={expanded.name}>
            <div className="h-[min(78vh,56.4vw)] w-full">
              <CameraTile instanceId={instanceId} camera={expanded} quality="main" />
            </div>
          </DialogContent>
        </Dialog>
      )}
    </div>
  );
}

function Header({ name, status, baseUrl }: { name: string; status: string; baseUrl: string | null }) {
  const { t } = useTranslation();
  return (
    <div className="flex flex-wrap items-center gap-3">
      <span className="bg-accent-soft text-accent-soft-fg flex size-10 items-center justify-center rounded-xl">
        <Cctv className="size-5" />
      </span>
      <div className="min-w-0 flex-1">
        <h1 className="text-fg flex items-center gap-2.5 text-xl font-semibold tracking-tight">
          {name}
          <Badge tone={statusTone[status as keyof typeof statusTone] ?? "neutral"}>
            <StatusDot tone={statusTone[status as keyof typeof statusTone] ?? "neutral"} pulse={status !== "connected"} />
            {t(`integrations.status.${status}`)}
          </Badge>
        </h1>
        <p className="text-fg-muted text-sm">{t("nvr.subtitle")}</p>
      </div>
      {baseUrl && (
        <a
          href={baseUrl}
          target="_blank"
          rel="noopener noreferrer"
          className="border-edge bg-surface hover:bg-surface-hover text-fg flex items-center gap-1.5 rounded-md border px-3 py-1.5 text-[13px] font-medium shadow-1 transition-colors"
        >
          <ExternalLink className="size-3.5" />
          {t("nvr.openAperture")}
        </a>
      )}
    </div>
  );
}

/** The dot on a live tile answers "is the live picture healthy" — green
 * while playing, amber while connecting, red on error. Recording health is
 * a different fact and gets its own label; folding it into the dot made
 * perfectly-live cameras look broken. */
function tileTone(live: LiveState): "success" | "warning" | "danger" {
  if (live.phase === "error") return "danger";
  if (live.phase === "connecting") return "warning";
  return "success";
}

/** Recording label: only cameras that are supposed to record can be "not
 * recording"; live-only cameras say so instead of counting as broken. */
function recordingLabel(t: (k: string, o?: Record<string, unknown>) => string, cam: ApertureCamera) {
  if (!cam.recordingExpected) return { text: t("nvr.grid.liveOnly"), tone: "text-white/50" };
  if (!cam.recordingOk || cam.watchdogStale) return { text: t("nvr.grid.notRecording"), tone: "text-red-400" };
  if (cam.coveragePercent < 90) {
    return { text: t("nvr.grid.coverage", { pct: cam.coveragePercent }), tone: "text-amber-300/90" };
  }
  return { text: t("nvr.grid.coverage", { pct: cam.coveragePercent }), tone: "text-white/65" };
}

function CameraTile({
  instanceId,
  camera,
  quality,
  onExpand,
}: {
  instanceId: string;
  camera: ApertureCamera;
  quality: "sub" | "main";
  onExpand?: () => void;
}) {
  const { t } = useTranslation();
  const containerRef = useRef<HTMLDivElement>(null);
  const [live, setLive] = useState<LiveState>({ phase: "connecting" });

  useEffect(() => {
    const container = containerRef.current;
    if (!container) return;
    const key = `${instanceId}/${camera.id}/${quality}`;
    const engine = acquireLive(
      key,
      container,
      async (useSub) => {
        const grant = unwrap(
          await api.POST("/nvr/{id}/cameras/{cameraId}/stream-token", {
            params: { path: { id: instanceId, cameraId: camera.id } },
          }),
        );
        return useSub && grant.hasSub ? `${grant.wsUrl}&q=sub` : grant.wsUrl;
      },
      quality === "sub",
    );
    const unsubscribe = engine.onState(setLive);
    return () => {
      unsubscribe();
      releaseLive(key);
    };
  }, [instanceId, camera.id, quality]);

  const tone = tileTone(live);
  const rec = recordingLabel(t, camera);

  return (
    <div
      className={cn(
        "border-edge group relative overflow-hidden rounded-lg border bg-black",
        quality === "sub" && "aspect-video cursor-pointer",
        quality === "main" && "size-full",
      )}
      onClick={onExpand}
      role={onExpand ? "button" : undefined}
      aria-label={onExpand ? t("nvr.grid.expand", { name: camera.name }) : undefined}
    >
      {/* The engine's <video> is appended here — attached before connecting. */}
      <div ref={containerRef} className="absolute inset-0" />

      {live.phase !== "playing" && (
        <div className="absolute inset-0 flex flex-col items-center justify-center gap-2 px-4 text-center">
          {live.phase === "connecting" ? (
            <>
              <Loader2 className="size-5 animate-spin text-white/70" />
              <p className="text-[12.5px] text-white/70">{t("nvr.grid.connecting")}</p>
            </>
          ) : (
            <>
              <AlertTriangle className="size-5 text-amber-400" />
              <p className="text-[12.5px] text-white/85">{live.reason}</p>
              <p className="text-[11.5px] text-white/50">
                {t("nvr.grid.retrying", { seconds: Math.round(live.retryInMs / 1000) })}
              </p>
            </>
          )}
        </div>
      )}

      <div className="absolute inset-x-0 bottom-0 flex items-center justify-between gap-2 bg-gradient-to-t from-black/75 to-transparent px-3 pt-6 pb-2">
        <span className="flex min-w-0 items-center gap-2">
          <StatusDot tone={tone} pulse={tone !== "success"} />
          <span className="truncate text-[13px] font-medium text-white">{camera.name}</span>
        </span>
        <span className={cn("shrink-0 text-[11.5px]", rec.tone)}>{rec.text}</span>
      </div>
    </div>
  );
}
