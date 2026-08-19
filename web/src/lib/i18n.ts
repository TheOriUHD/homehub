import i18n from "i18next";
import { initReactI18next } from "react-i18next";
import en from "@/locales/en.json";
import de from "@/locales/de.json";

// English ships built-in and stays the default; German is a complete
// translation. The user's choice persists client-side (hh-lang).
export const resources = {
  en: { translation: en },
  de: { translation: de },
} as const;

let stored = "en";
try {
  stored = localStorage.getItem("hh-lang") ?? "en";
} catch {
  /* private mode */
}

void i18n.use(initReactI18next).init({
  resources,
  lng: stored in resources ? stored : "en",
  fallbackLng: "en",
  interpolation: {
    // React already escapes rendered strings.
    escapeValue: false,
  },
});

export default i18n;
