import { createContext, useCallback, useContext, useRef, useState, type ReactNode } from "react";
import { CheckCircle2, Info, AlertTriangle, XCircle, X } from "lucide-react";
import { cn } from "@/lib/utils";

type ToastTone = "success" | "info" | "warning" | "error";

type ToastItem = {
  id: number;
  tone: ToastTone;
  title: string;
  body?: string;
};

type ToastFn = (tone: ToastTone, title: string, body?: string) => void;

const ToastContext = createContext<ToastFn | null>(null);

const icons: Record<ToastTone, typeof Info> = {
  success: CheckCircle2,
  info: Info,
  warning: AlertTriangle,
  error: XCircle,
};

const iconTones: Record<ToastTone, string> = {
  success: "text-success",
  info: "text-info",
  warning: "text-warning",
  error: "text-danger",
};

export function ToastProvider({ children }: { children: ReactNode }) {
  const [items, setItems] = useState<ToastItem[]>([]);
  const nextId = useRef(1);

  const push = useCallback<ToastFn>((tone, title, body) => {
    const id = nextId.current++;
    setItems((prev) => [...prev.slice(-3), { id, tone, title, body }]);
    window.setTimeout(() => {
      setItems((prev) => prev.filter((t) => t.id !== id));
    }, tone === "error" ? 8000 : 4500);
  }, []);

  return (
    <ToastContext.Provider value={push}>
      {children}
      <div
        aria-live="polite"
        className="pointer-events-none fixed right-4 bottom-4 z-[60] flex w-[calc(100vw-2rem)] max-w-sm flex-col gap-2"
      >
        {items.map((item) => {
          const Icon = icons[item.tone];
          return (
            <div
              key={item.id}
              className="bg-overlay border-edge pointer-events-auto flex items-start gap-3 rounded-lg border p-3.5 shadow-3 animate-[hh-slide-up_180ms_var(--ease-out-quick)]"
            >
              <Icon className={cn("mt-0.5 size-4.5 shrink-0", iconTones[item.tone])} />
              <div className="min-w-0 flex-1">
                <p className="text-fg text-sm font-medium">{item.title}</p>
                {item.body && <p className="text-fg-muted mt-0.5 text-[13px] break-words">{item.body}</p>}
              </div>
              <button
                onClick={() => setItems((prev) => prev.filter((t) => t.id !== item.id))}
                className="interactive-muted flex size-6 shrink-0 cursor-pointer items-center justify-center"
                aria-label="Dismiss"
              >
                <X className="size-3.5" />
              </button>
            </div>
          );
        })}
      </div>
    </ToastContext.Provider>
  );
}

export function useToast(): ToastFn {
  const fn = useContext(ToastContext);
  if (!fn) throw new Error("useToast outside ToastProvider");
  return fn;
}
