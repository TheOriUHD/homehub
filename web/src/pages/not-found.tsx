import { useNavigate } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { Compass } from "lucide-react";
import { Button } from "@/ui/button";

export function NotFoundPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  return (
    <div className="bg-bg flex min-h-full flex-col items-center justify-center px-4 text-center">
      <div className="bg-bg-subtle text-fg-subtle mb-4 flex size-12 items-center justify-center rounded-2xl">
        <Compass className="size-6" />
      </div>
      <h1 className="text-fg text-lg font-semibold">{t("errors.notFound.title")}</h1>
      <p className="text-fg-muted mt-1 max-w-sm text-sm">{t("errors.notFound.body")}</p>
      <Button variant="primary" className="mt-5" onClick={() => void navigate({ to: "/" })}>
        {t("errors.notFound.home")}
      </Button>
    </div>
  );
}
