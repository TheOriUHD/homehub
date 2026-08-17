import i18n from "i18next";
import { initReactI18next } from "react-i18next";
import en from "@/locales/en.json";

// English ships built-in; additional locales drop into src/locales and
// register here.
export const resources = {
  en: { translation: en },
} as const;

void i18n.use(initReactI18next).init({
  resources,
  lng: "en",
  fallbackLng: "en",
  interpolation: {
    // React already escapes rendered strings.
    escapeValue: false,
  },
});

export default i18n;
