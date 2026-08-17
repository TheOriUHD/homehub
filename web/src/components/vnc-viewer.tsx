import { useEffect, useRef, useState } from "react";
import RFB, { type RFBDisconnectDetail, type RFBSecurityFailureDetail } from "@novnc/novnc";
import { AlertTriangle, Loader2, MonitorOff, RotateCw, ShieldAlert } from "lucide-react";
import { useTranslation } from "react-i18next";
import { cn } from "@/lib/utils";
import { Button } from "@/ui/button";

type VncStatus =
  | { kind: "connecting" }
  | { kind: "connected" }
  | { kind: "closed"; clean: boolean }
  | { kind: "failed"; reason: string };

/**
 * VncViewer renders a remote desktop over a WebSocket bridge using noVNC.
 * The bridge authenticates upstream, so no credentials pass through here.
 * It never reconnects on its own — a dropped session shows an overlay with a
 * manual reconnect button.
 */
export function VncViewer({
  wsUrl,
  credential,
  className,
}: {
  wsUrl: string;
  /** One-time RFB password (e.g. a Proxmox VNC ticket) answered during VNC authentication. */
  credential?: string;
  className?: string;
}) {
  const { t } = useTranslation();
  const containerRef = useRef<HTMLDivElement>(null);
  const [status, setStatus] = useState<VncStatus>({ kind: "connecting" });
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    setStatus({ kind: "connecting" });
    const rfb = new RFB(el, wsUrl, credential ? { credentials: { password: credential } } : undefined);
    rfb.scaleViewport = true;
    // Never ask the server to resize the session: QEMU answers by padding a
    // fixed-mode display (e.g. a VGA text console) with dead space instead of
    // letting the client scale it. Scale-to-fit with the aspect locked is
    // what actually fills the pane.
    rfb.resizeSession = false;
    // Let the wrapper's token background show through the letterbox area.
    rfb.background = "transparent";

    const onConnect = () => setStatus({ kind: "connected" });
    const onDisconnect = (e: CustomEvent<RFBDisconnectDetail>) => {
      // A security failure already explained the disconnect that follows it.
      setStatus((prev) => (prev.kind === "failed" ? prev : { kind: "closed", clean: e.detail.clean }));
    };
    const onSecurityFailure = (e: CustomEvent<RFBSecurityFailureDetail>) => {
      setStatus({ kind: "failed", reason: e.detail.reason ?? "" });
    };
    rfb.addEventListener("connect", onConnect);
    rfb.addEventListener("disconnect", onDisconnect);
    rfb.addEventListener("securityfailure", onSecurityFailure);

    return () => {
      rfb.removeEventListener("connect", onConnect);
      rfb.removeEventListener("disconnect", onDisconnect);
      rfb.removeEventListener("securityfailure", onSecurityFailure);
      try {
        rfb.disconnect();
      } catch {
        // Already disconnected — nothing left to tear down.
      }
    };
  }, [wsUrl, attempt]);

  const ended = status.kind === "closed" || status.kind === "failed";
  const error = status.kind === "failed" || (status.kind === "closed" && !status.clean);
  const Icon = status.kind === "failed" ? ShieldAlert : error ? AlertTriangle : MonitorOff;
  const message =
    status.kind === "failed"
      ? status.reason
        ? t("term.vncSecurityFailure", { reason: status.reason })
        : t("term.vncSecurityFailureUnknown")
      : status.kind === "closed" && !status.clean
        ? t("term.vncDisconnectedUnclean")
        : t("term.vncDisconnected");

  return (
    <div className={cn("bg-bg-subtle relative h-full min-h-0 w-full overflow-hidden", className)}>
      <div ref={containerRef} className="absolute inset-0" />

      {status.kind === "connecting" && (
        <div
          role="status"
          className="bg-bg/60 absolute inset-0 z-10 flex items-center justify-center animate-[hh-fade-in_150ms_var(--ease-out-quick)]"
        >
          <Loader2 aria-hidden className="text-fg-subtle size-5 animate-spin" />
          <span className="sr-only">{t("term.connecting")}</span>
        </div>
      )}

      {ended && (
        <div
          role="alert"
          className="bg-bg/80 absolute inset-0 z-10 flex flex-col items-center justify-center px-6 text-center backdrop-blur-[2px] animate-[hh-fade-in_150ms_var(--ease-out-quick)]"
        >
          <div
            className={cn(
              "mb-3 flex size-11 items-center justify-center rounded-xl",
              error ? "bg-danger-soft text-danger-soft-fg" : "bg-bg-subtle text-fg-subtle",
            )}
          >
            <Icon className="size-5" />
          </div>
          <p className="text-fg max-w-md text-sm font-medium">{message}</p>
          <Button variant="secondary" size="sm" className="mt-4" onClick={() => setAttempt((a) => a + 1)}>
            <RotateCw aria-hidden className="size-3.5" />
            {t("term.reconnect")}
          </Button>
        </div>
      )}
    </div>
  );
}
