import { useMutation } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { KeyRound, TriangleAlert } from "lucide-react";
import { api, unwrap, isApiError } from "@/lib/api";
import { Card, CardBody, CardHeader } from "@/ui/card";
import { Button } from "@/ui/button";
import { Secret } from "@/ui/misc";
import { useToast } from "@/ui/toast";

/**
 * EncryptionKeyCard exports the age identity that decrypts encrypted archives.
 * Rendered only for users with configure on system/backups; the export itself
 * is audited server-side.
 */
export function EncryptionKeyCard() {
  const { t } = useTranslation();
  const toast = useToast();

  const reveal = useMutation({
    mutationFn: async () => unwrap(await api.GET("/backups/encryption-key")),
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  return (
    <Card className="max-w-2xl">
      <CardHeader title={t("backups.key.title")} description={t("backups.key.intro")} />
      <CardBody className="space-y-3">
        {reveal.data ? (
          <>
            <p className="bg-warning-soft text-warning-soft-fg flex items-start gap-2.5 rounded-lg px-4 py-3 text-[13px]">
              <TriangleAlert className="mt-0.5 size-4 shrink-0" />
              {t("backups.key.warning")}
            </p>
            <Secret value={reveal.data.key} />
            <div className="flex justify-end">
              <Button size="sm" variant="ghost" onClick={() => reveal.reset()}>
                {t("backups.key.hide")}
              </Button>
            </div>
          </>
        ) : (
          <div className="flex flex-wrap items-center justify-between gap-3">
            <p className="text-fg-subtle text-[13px]">{t("backups.key.auditNote")}</p>
            <Button variant="secondary" loading={reveal.isPending} onClick={() => reveal.mutate()}>
              <KeyRound className="size-4" />
              {t("backups.key.reveal")}
            </Button>
          </div>
        )}
      </CardBody>
    </Card>
  );
}
