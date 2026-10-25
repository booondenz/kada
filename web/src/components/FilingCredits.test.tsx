import { renderToStaticMarkup } from "react-dom/server";
import { MemoryRouter } from "react-router";
import { describe, expect, it } from "vitest";
import { I18nProvider } from "@/lib/i18n";
import { FilingCredits } from "./FilingCredits";
import { ICP_NUMBER } from "./IcpNotice";
import { PUBLIC_SECURITY_NUMBER } from "./PublicSecurityNotice";

// The sidebar used to spend three rows on these four items. What this file guards is the shape of the
// replacement: one control, closed by default, with both filing numbers still in the document. They are a
// legal requirement, and the failure worth a test is a redesign that removes them rather than folds them.
const html = renderToStaticMarkup(
  <I18nProvider>
    <MemoryRouter>
      <FilingCredits />
    </MemoryRouter>
  </I18nProvider>,
);

describe("FilingCredits", () => {
  it("is one control instead of three rows of footer text", () => {
    expect(html).toContain("备案与隐私");
    expect(html).toContain('aria-expanded="false"');
    expect(html).toContain('aria-controls="filing-credits"');
  });

  it("starts collapsed", () => {
    expect(html).toMatch(/id="filing-credits"[^>]*hidden/);
  });

  it("keeps both filing numbers in the document while collapsed", () => {
    expect(html).toContain(ICP_NUMBER);
    expect(html).toContain(PUBLIC_SECURITY_NUMBER);
  });

  it("keeps the privacy policy and the report form one click away", () => {
    expect(html).toContain('href="/privacy"');
    expect(html).toContain('href="/report"');
  });
});
