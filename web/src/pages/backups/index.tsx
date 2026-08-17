import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { isApiError } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { Card } from "@/ui/card";
import { ErrorState } from "@/ui/states";
import { BACKUPS_RES, backupTargetsQuery } from "./shared";
import { TargetsSection } from "./targets";
import { SchedulesSection } from "./schedules";
import { RunsSection } from "./history";
import { EncryptionKeyCard } from "./encryption-key";

export function BackupsPage() {
  const { t } = useTranslation();
  const { can } = useAuth();

  const targets = useQuery(backupTargetsQuery());

  // Deep link without permission: the server already said no — show that
  // instead of firing the remaining section queries into the same wall.
  const forbidden = targets.isError && isApiError(targets.error) && targets.error.status === 403;

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-fg text-xl font-semibold tracking-tight">{t("backups.title")}</h1>
        <p className="text-fg-muted mt-1 text-sm">{t("backups.subtitle")}</p>
      </div>

      {forbidden ? (
        <Card>
          <ErrorState error={targets.error} />
        </Card>
      ) : (
        <>
          <TargetsSection targets={targets} />
          <SchedulesSection targets={targets.data ?? []} />
          <RunsSection />
          {can(BACKUPS_RES, "configure") && <EncryptionKeyCard />}
        </>
      )}
    </div>
  );
}
