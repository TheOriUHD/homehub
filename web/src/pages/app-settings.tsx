import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { api, unwrap, isApiError } from "@/lib/api";
import { Card, CardBody, CardHeader } from "@/ui/card";
import { Button } from "@/ui/button";
import { Input } from "@/ui/input";
import { Field } from "@/ui/field";
import { ErrorState } from "@/ui/states";
import { Skeleton } from "@/ui/skeleton";
import { useToast } from "@/ui/toast";

export function AppSettingsPage() {
  const { t } = useTranslation();
  const toast = useToast();
  const queryClient = useQueryClient();

  const settings = useQuery({
    queryKey: ["settings"],
    queryFn: async () => unwrap(await api.GET("/settings")),
  });

  const [appUrl, setAppUrl] = useState("");
  const [sessionTtl, setSessionTtl] = useState("");
  useEffect(() => {
    if (settings.data) {
      setAppUrl(settings.data.appUrl);
      setSessionTtl(String(settings.data.sessionTtlHours));
    }
  }, [settings.data]);

  const save = useMutation({
    mutationFn: async () =>
      unwrap(
        await api.PATCH("/settings", {
          body: { appUrl, sessionTtlHours: Number(sessionTtl) },
        }),
      ),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["settings"] });
      toast("success", t("settingsPage.saved"));
    },
  });

  const error = save.error && isApiError(save.error) ? save.error.message : null;

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-fg text-xl font-semibold tracking-tight">{t("settingsPage.title")}</h1>
        <p className="text-fg-muted mt-1 text-sm">{t("settingsPage.subtitle")}</p>
      </div>

      <Card className="max-w-xl">
        {settings.isLoading ? (
          <CardBody className="space-y-4">
            <Skeleton className="h-9 w-full" />
            <Skeleton className="h-9 w-full" />
          </CardBody>
        ) : settings.isError ? (
          <ErrorState error={settings.error} onRetry={() => void settings.refetch()} />
        ) : (
          <>
            <CardHeader title={t("settingsPage.title")} />
            <CardBody>
              <form
                className="space-y-4"
                onSubmit={(e) => {
                  e.preventDefault();
                  save.mutate();
                }}
              >
                <Field label={t("settingsPage.appUrl")} hint={t("settingsPage.appUrlHint")} error={error}>
                  {(p) => (
                    <Input
                      {...p}
                      type="url"
                      placeholder="https://hub.example.com"
                      value={appUrl}
                      onChange={(e) => setAppUrl(e.target.value)}
                    />
                  )}
                </Field>
                <Field label={t("settingsPage.sessionTtl")} hint={t("settingsPage.sessionTtlHint")}>
                  {(p) => (
                    <Input
                      {...p}
                      type="number"
                      min={1}
                      max={8760}
                      required
                      className="max-w-32"
                      value={sessionTtl}
                      onChange={(e) => setSessionTtl(e.target.value)}
                    />
                  )}
                </Field>
                <div className="flex justify-end">
                  <Button type="submit" variant="primary" loading={save.isPending}>
                    {save.isPending ? t("common.saving") : t("common.save")}
                  </Button>
                </div>
              </form>
            </CardBody>
          </>
        )}
      </Card>
    </div>
  );
}
