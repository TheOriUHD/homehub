import { useEffect } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import * as PopoverPrimitive from "@radix-ui/react-popover";
import { Bell, Inbox } from "lucide-react";
import { useTranslation } from "react-i18next";
import { api, unwrap, type AppNotification } from "@/lib/api";
import { live } from "@/lib/ws";
import { cn, formatRelative } from "@/lib/utils";
import { useToast } from "@/ui/toast";
import { EmptyState } from "@/ui/states";
import { SkeletonRows } from "@/ui/skeleton";
import { Button } from "@/ui/button";

const severityDot: Record<AppNotification["severity"], string> = {
  info: "bg-info",
  success: "bg-success",
  warning: "bg-warning",
  error: "bg-danger",
};

export function NotificationsBell() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const toast = useToast();

  const query = useQuery({
    queryKey: ["notifications"],
    queryFn: async () => unwrap(await api.GET("/notifications", { params: { query: { limit: 20 } } })),
  });

  // Live inbox: new events refresh the list and surface a toast.
  useEffect(() => {
    return live.subscribe("notifications", (data) => {
      void queryClient.invalidateQueries({ queryKey: ["notifications"] });
      const n = data as AppNotification | undefined;
      if (n?.title) {
        toast(n.severity === "error" ? "error" : n.severity === "warning" ? "warning" : "info", n.title, n.body);
      }
    });
  }, [queryClient, toast]);

  const markAll = useMutation({
    mutationFn: async () => unwrap(await api.POST("/notifications/read", { body: { all: true } })),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ["notifications"] }),
  });

  const unread = query.data?.unreadCount ?? 0;

  return (
    <PopoverPrimitive.Root>
      <PopoverPrimitive.Trigger asChild>
        <button
          className="interactive-muted relative flex size-8.5 cursor-pointer items-center justify-center"
          aria-label={t("nav.notifications")}
        >
          <Bell className="size-4.5" />
          {unread > 0 && (
            <span className="bg-accent text-accent-fg absolute -top-0.5 -right-0.5 flex h-4 min-w-4 items-center justify-center rounded-full px-1 text-[10px] font-semibold">
              {unread > 99 ? "99+" : unread}
            </span>
          )}
        </button>
      </PopoverPrimitive.Trigger>
      <PopoverPrimitive.Portal>
        <PopoverPrimitive.Content
          align="end"
          sideOffset={8}
          className="bg-overlay border-edge z-50 w-[calc(100vw-2rem)] max-w-sm rounded-xl border shadow-3 animate-[hh-pop-in_150ms_var(--ease-out-quick)]"
        >
          <div className="border-edge flex items-center justify-between border-b px-4 py-3">
            <h3 className="text-fg text-sm font-semibold">{t("notifications.title")}</h3>
            {unread > 0 && (
              <Button size="sm" variant="ghost" loading={markAll.isPending} onClick={() => markAll.mutate()}>
                {t("notifications.markAllRead")}
              </Button>
            )}
          </div>
          <div className="max-h-96 overflow-y-auto">
            {query.isLoading ? (
              <SkeletonRows rows={3} className="p-4" />
            ) : !query.data || query.data.notifications.length === 0 ? (
              <EmptyState
                icon={Inbox}
                title={t("notifications.empty.title")}
                body={t("notifications.empty.body")}
                className="py-10"
              />
            ) : (
              <ul>
                {query.data.notifications.map((n) => (
                  <li key={n.id} className="border-edge flex gap-3 border-b px-4 py-3 last:border-b-0">
                    <span className={cn("mt-1.5 size-2 shrink-0 rounded-full", severityDot[n.severity], n.read && "opacity-30")} />
                    <div className="min-w-0 flex-1">
                      <p className={cn("text-sm", n.read ? "text-fg-muted" : "text-fg font-medium")}>{n.title}</p>
                      {n.body && <p className="text-fg-subtle mt-0.5 text-[13px]">{n.body}</p>}
                      <p className="text-fg-subtle mt-1 text-xs">{formatRelative(n.ts)}</p>
                    </div>
                  </li>
                ))}
              </ul>
            )}
          </div>
        </PopoverPrimitive.Content>
      </PopoverPrimitive.Portal>
    </PopoverPrimitive.Root>
  );
}
