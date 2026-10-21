import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { ICP_NUMBER } from "@/components/IcpNotice";
import { AuthCard } from "./AuthCard";

// The sign-in shell is the page the site opens on, so it is where a filing spot check looks for the number:
// `/` has no landing page and sends a visitor here. The assertion lives on the shell rather than on the
// page because the shell is what every mode of the form renders inside, and because a redesign that drops
// the footer would otherwise only be noticed by the regulator.
describe("AuthCard", () => {
  const html = renderToStaticMarkup(
    <AuthCard title="登录 Kada" subtitle="智能短链接管理平台" footer="注册">
      <form />
    </AuthCard>,
  );

  it("carries the filing number", () => {
    expect(html).toContain(ICP_NUMBER);
  });

  it("renders the title, the subtitle and the mode switch it is given", () => {
    expect(html).toContain("登录 Kada");
    expect(html).toContain("智能短链接管理平台");
    expect(html).toContain("注册");
  });
});
