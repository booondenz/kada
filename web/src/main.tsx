import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { RouterProvider } from "react-router";
import { Toaster } from "sonner";

import { LocaleHtmlLang } from "@/components/LanguageSwitcher";
import { I18nProvider } from "@/lib/i18n/context";
import { ThemeProvider } from "@/lib/theme";
import { router } from "@/router";

import "@/globals.css";

// The theme attribute is already on <html> by the time this runs: index.html carries the script that sets
// it (see src/lib/theme/bootstrap.ts). Nothing here has to decide the theme before the first paint.
const container = document.getElementById("root");
if (!container) {
  throw new Error("the root element is missing from index.html");
}

createRoot(container).render(
  <StrictMode>
    <ThemeProvider>
      <I18nProvider>
        <LocaleHtmlLang />
        <RouterProvider router={router} />
        <Toaster position="top-center" richColors closeButton toastOptions={{ duration: 3000 }} />
      </I18nProvider>
    </ThemeProvider>
  </StrictMode>
);
