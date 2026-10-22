import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import {
  PUBLIC_SECURITY_NUMBER,
  PUBLIC_SECURITY_QUERY_URL,
  PublicSecurityNotice,
} from "./PublicSecurityNotice";

// The same rule as the ICP notice: the number is legal text, and the failure worth a test is a redesign that
// quietly drops it. Rendered to static markup, like the other component tests in this directory.
describe("PublicSecurityNotice", () => {
  const html = renderToStaticMarkup(<PublicSecurityNotice />);

  it("shows the filing number as filed", () => {
    expect(html).toContain(PUBLIC_SECURITY_NUMBER);
  });

  it("links to the filing office's public query", () => {
    expect(html).toContain(`href="${PUBLIC_SECURITY_QUERY_URL}"`);
  });

  it("opens the query in a new tab without handing it the opener", () => {
    expect(html).toContain('target="_blank"');
    expect(html).toContain("noreferrer");
  });
});
