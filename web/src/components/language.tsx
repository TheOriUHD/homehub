import { useState } from "react";
import { useTranslation } from "react-i18next";
import { cn } from "@/lib/utils";
import { Button } from "@/ui/button";
import { Dialog, DialogContent } from "@/ui/dialog";

/**
 * Language selection: a flag-based switcher (Account page) and the one-time
 * first-sign-in prompt. The choice lives client-side (hh-lang) and applies
 * instantly; English is and stays the default.
 */

export const LANG_KEY = "hh-lang";
const CHOSEN_KEY = "hh-lang-chosen";

export function storedLanguage(): string {
  try {
    return localStorage.getItem(LANG_KEY) ?? "en";
  } catch {
    return "en";
  }
}

export function languagePromptPending(): boolean {
  try {
    return localStorage.getItem(CHOSEN_KEY) === null;
  } catch {
    return false;
  }
}

function persistLanguage(lang: string) {
  try {
    localStorage.setItem(LANG_KEY, lang);
    localStorage.setItem(CHOSEN_KEY, "1");
  } catch {
    /* private mode */
  }
}

/** US flag, simplified. */
function FlagUS({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 60 40" className={className} aria-hidden>
      <rect width="60" height="40" fill="#b22234" rx="4" />
      {[1, 3, 5, 7, 9, 11].map((i) => (
        <rect key={i} y={(i * 40) / 13} width="60" height={40 / 13} fill="#fff" />
      ))}
      <rect width="26" height={(7 * 40) / 13} fill="#3c3b6e" rx="2" />
      {Array.from({ length: 15 }, (_, i) => {
        const row = Math.floor(i / 5);
        const col = i % 5;
        return (
          <circle key={i} cx={3.5 + col * 4.8} cy={3.6 + row * 7} r="1.15" fill="#fff" />
        );
      })}
    </svg>
  );
}

/** German flag. */
function FlagDE({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 60 40" className={className} aria-hidden>
      <rect width="60" height="40" fill="#000" rx="4" />
      <rect y="13.33" width="60" height="26.67" fill="#dd0000" />
      <rect y="26.66" width="60" height="13.34" fill="#ffce00" />
      <rect width="60" height="40" fill="none" rx="4" />
    </svg>
  );
}

const LANGS = [
  { code: "en", label: "English", Flag: FlagUS },
  { code: "de", label: "Deutsch", Flag: FlagDE },
] as const;

/** The flag switcher card body used on the Account page. */
export function LanguagePicker() {
  const { i18n } = useTranslation();
  const [active, setActive] = useState(i18n.language);

  const pick = (code: string) => {
    setActive(code);
    persistLanguage(code);
    void i18n.changeLanguage(code);
  };

  return (
    <div className="flex flex-wrap gap-2.5">
      {LANGS.map(({ code, label, Flag }) => (
        <button
          key={code}
          type="button"
          onClick={() => pick(code)}
          className={cn(
            "flex items-center gap-3 rounded-xl border px-4 py-2.5 transition-all",
            active === code
              ? "border-accent bg-accent-soft shadow-1"
              : "border-edge hover:border-edge-strong hover:bg-bg-subtle",
          )}
        >
          <Flag className="h-6 w-9 rounded-[4px] shadow-1" />
          <span
            className={cn(
              "text-[13.5px]",
              active === code ? "text-accent-soft-fg font-semibold" : "text-fg font-medium",
            )}
          >
            {label}
          </span>
        </button>
      ))}
    </div>
  );
}

/**
 * The one-time first-sign-in prompt. Always rendered in English (the
 * default) — the choice applies the moment a flag is clicked.
 */
export function LanguagePrompt({ onDone }: { onDone: () => void }) {
  const { i18n } = useTranslation();

  const choose = (code: string) => {
    persistLanguage(code);
    void i18n.changeLanguage(code);
    onDone();
  };

  return (
    <Dialog open onOpenChange={(o) => !o && choose(i18n.language)}>
      <DialogContent title="Choose your language" className="max-w-sm">
        <p className="text-fg-muted mb-4 text-[13px]">
          HomeHub speaks English and German. Pick what you prefer — you can
          change it anytime in your account settings.
        </p>
        <div className="space-y-2">
          {LANGS.map(({ code, label, Flag }) => (
            <button
              key={code}
              type="button"
              onClick={() => choose(code)}
              className="border-edge hover:border-accent hover:bg-accent-soft/40 flex w-full items-center gap-3.5 rounded-xl border px-4 py-3 transition-all"
            >
              <Flag className="h-7 w-10.5 rounded-[5px] shadow-1" />
              <span className="text-fg text-[14.5px] font-medium">{label}</span>
            </button>
          ))}
        </div>
        <div className="mt-3 flex justify-end">
          <Button variant="ghost" size="sm" onClick={() => choose("en")}>
            Keep English
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}
