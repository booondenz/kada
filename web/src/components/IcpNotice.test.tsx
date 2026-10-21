import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { ICP_NUMBER, ICP_QUERY_URL, IcpNotice } from "./IcpNotice";

// The number and the link are a legal requirement, not copy: the failure worth a test is a redesign that
// quietly drops the footer, and it is cheaper to catch here than at the next spot check. Rendered to static
// markup, the same way the other component tests in this directory do it - no DOM, no extra dependency.
describe("IcpNotice", () => {
  const html = renderToStaticMarkup(<IcpNotice />);

  it("shows the filing number as filed", () => {
    expect(html).toContain(ICP_NUMBER);
  });

  it("links to the filing office's public query", () => {
    expect(html).toContain(`href="${ICP_QUERY_URL}"`);
  });

  it("opens the query in a new tab without handing it the opener", () => {
    expect(html).toContain('target="_blank"');
    expect(html).toContain("noreferrer");
  });
});
