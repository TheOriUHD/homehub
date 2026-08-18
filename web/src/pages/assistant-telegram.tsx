import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { CircleCheck, CircleX, Link2, Send, Trash2 } from "lucide-react";
import { api, isApiError, unwrap } from "@/lib/api";
import { Button } from "@/ui/button";
import { Dialog, DialogContent } from "@/ui/dialog";
import { Field } from "@/ui/field";
import { Input } from "@/ui/input";
import { Switch } from "@/ui/switch";
import { useToast } from "@/ui/toast";

/**
 * Telegram bridge setup: paste a BotFather token, flip the switch, link a
 * chat with a short-lived code. Linked chats talk to the assistant under
 * the linked user's permissions; ! or / prefixed messages are command
 * triggers (routing configured separately). While a link code is pending
 * the panel polls so the freshly linked chat pops in without a refresh.
 */
export function TelegramDialog({ canConfigure, onClose }: { canConfigure: boolean; onClose: () => void }) {
  const { t } = useTranslation();
  const toast = useToast();
  const qc = useQueryClient();
  const [token, setToken] = useState("");
  const [enabled, setEnabled] = useState<boolean | null>(null);
  const [code, setCode] = useState("");

  const state = useQuery({
    queryKey: ["telegram"],
    queryFn: async () => unwrap(await api.GET("/telegram/settings")),
  });
  const data = state.data;
  const isOn = enabled ?? data?.enabled ?? false;

  const save = useMutation({
    mutationFn: async () =>
      unwrap(
        await api.PUT("/telegram/settings", {
          body: { enabled: isOn, ...(token.trim() ? { token: token.trim() } : {}) },
        }),
      ),
    onSuccess: () => {
      setToken("");
      toast("success", t("assistant.telegram.saved"));
      void qc.invalidateQueries({ queryKey: ["telegram"] });
    },
    onError: (err) => toast("error", isApiError(err) ? err.message : String(err)),
  });

  const confirm = useMutation({
    mutationFn: async () => unwrap(await api.POST("/telegram/link-code", { body: { code: code.trim() } })),
    onSuccess: (res) => {
      setCode("");
      toast("success", t("assistant.telegram.linkedAs", { label: res.label }));
      void qc.invalidateQueries({ queryKey: ["telegram"] });
    },
    onError: (err) => toast("error", isApiError(err) ? err.message : String(err)),
  });

  const unlink = useMutation({
    mutationFn: async (chatId: number) =>
      unwrap(await api.DELETE("/telegram/chats/{chatId}", { params: { path: { chatId } } })),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ["telegram"] }),
    onError: (err) => toast("error", isApiError(err) ? err.message : String(err)),
  });

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent
        title={t("assistant.telegram.title")}
        description={t("assistant.telegram.subtitle")}
        className="max-w-lg"
      >
        <div className="space-y-5">
          {/* status */}
          {data && (data.botUsername || data.botError || data.enabled) && (
            <div className="bg-bg-subtle flex items-center gap-2 rounded-lg px-3 py-2 text-[13px]">
              {data.botUsername ? (
                <>
                  <CircleCheck className="text-success size-4 shrink-0" />
                  <span className="text-fg">
                    {t("assistant.telegram.running", { bot: "@" + data.botUsername })}
                  </span>
                </>
              ) : data.botError ? (
                <>
                  <CircleX className="text-danger size-4 shrink-0" />
                  <span className="text-danger min-w-0 break-words">{data.botError}</span>
                </>
              ) : (
                <span className="text-fg-muted">{t("assistant.telegram.starting")}</span>
              )}
            </div>
          )}

          {/* setup steps */}
          <ol className="text-fg-muted list-decimal space-y-1.5 pl-5 text-[13px]">
            <li>
              {t("assistant.telegram.step1a")}{" "}
              <a
                href="https://t.me/BotFather"
                target="_blank"
                rel="noreferrer"
                className="text-accent hover:underline"
              >
                @BotFather
              </a>
              {t("assistant.telegram.step1b")}
            </li>
            <li>{t("assistant.telegram.step2")}</li>
            <li>{t("assistant.telegram.step3")}</li>
            <li>{t("assistant.telegram.step4")}</li>
          </ol>

          {canConfigure && (
            <>
              <Field label={t("assistant.telegram.token")} hint={data?.tokenSet ? t("assistant.telegram.tokenStored") : undefined}>
                {(p) => (
                  <Input
                    {...p}
                    type="password"
                    value={token}
                    onChange={(e) => setToken(e.target.value)}
                    placeholder={data?.tokenSet ? "••••••••" : "123456789:AAF…"}
                    autoComplete="off"
                  />
                )}
              </Field>
              <div className="flex items-center justify-between gap-3">
                <label className="text-fg flex items-center gap-2.5 text-sm">
                  <Switch checked={isOn} onCheckedChange={(v) => setEnabled(v)} />
                  {t("assistant.telegram.enable")}
                </label>
                <Button
                  variant="primary"
                  size="sm"
                  disabled={save.isPending || (!data?.tokenSet && !token.trim() && isOn)}
                  onClick={() => save.mutate()}
                >
                  {t("common.save")}
                </Button>
              </div>
            </>
          )}

          {/* linked chats */}
          <div className="space-y-2">
            <p className="text-fg text-[13px] font-semibold">{t("assistant.telegram.chats")}</p>
            {(data?.chats.length ?? 0) === 0 && (
              <p className="text-fg-subtle text-[13px]">{t("assistant.telegram.noChats")}</p>
            )}
            {data?.chats.map((chat) => (
              <div key={String(chat.chatId)} className="border-edge flex items-center gap-2.5 rounded-lg border px-3 py-2">
                <Send className="text-fg-subtle size-3.5 shrink-0" />
                <span className="text-fg min-w-0 flex-1 truncate text-[13px]">{chat.label || chat.chatId}</span>
                <span className="text-fg-subtle shrink-0 text-[12px]">{chat.userName}</span>
                {canConfigure && (
                  <button
                    type="button"
                    className="text-fg-subtle hover:text-danger p-1"
                    onClick={() => unlink.mutate(chat.chatId)}
                    aria-label={t("common.delete")}
                  >
                    <Trash2 className="size-3.5" />
                  </button>
                )}
              </div>
            ))}

            <div className="border-edge space-y-2 rounded-lg border border-dashed px-3 py-2.5">
              <p className="text-fg-muted text-[13px]">
                {t("assistant.telegram.enterCode", { bot: data?.botUsername ? "@" + data.botUsername : t("assistant.telegram.yourBot") })}
              </p>
              <div className="flex items-center gap-2">
                <Input
                  value={code}
                  onChange={(e) => setCode(e.target.value.toUpperCase())}
                  placeholder="ABCD2345"
                  autoComplete="off"
                  spellCheck={false}
                  className="font-mono tracking-widest uppercase"
                  onKeyDown={(e) => {
                    if (e.key === "Enter" && code.trim()) confirm.mutate();
                  }}
                />
                <Button
                  variant="primary"
                  size="sm"
                  disabled={confirm.isPending || code.trim().length < 4 || !data?.botUsername}
                  onClick={() => confirm.mutate()}
                >
                  <Link2 className="size-3.5" />
                  {t("assistant.telegram.linkChat")}
                </Button>
              </div>
            </div>
          </div>

          <p className="text-fg-subtle text-[12px]">{t("assistant.telegram.modes")}</p>
        </div>
      </DialogContent>
    </Dialog>
  );
}
