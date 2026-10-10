import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { AiAnswer } from "./AiAnswer";

// The parser is tested on its own; these render it, because the component's branch order is where a
// construct goes missing: a table and a code block carry no spans, so a blank-line check placed above
// them would swallow both and show nothing. Rendering to static markup needs no DOM and no new
// dependency - react-dom is already here.
const html = (text: string): string => renderToStaticMarkup(<AiAnswer text={text} />);

describe("AiAnswer", () => {
  it("renders a table as a real table, with no pipes left over", () => {
    const out = html("| 组件 | 技术 |\n| --- | --- |\n| Go 后端 | Gin + GORM |");

    expect(out).toContain("<table");
    expect(out).toContain("<th");
    expect(out).toContain("<td");
    expect(out).toContain("Go 后端");
    expect(out).toContain("Gin + GORM");
    expect(out).not.toContain("|");
  });

  it("pads a row that arrives with fewer cells than the header", () => {
    const out = html("| a | b |\n| --- | --- |\n| 1 |");

    expect(out.match(/<td/g)).toHaveLength(2);
  });

  it("keeps the inline styling of a cell", () => {
    const out = html("| **粗体** | `code` |\n| --- | --- |");

    expect(out).toContain("<strong");
    expect(out).toContain("<code");
    expect(out).not.toContain("**");
  });

  it("renders a fence as preformatted text and leaves its contents alone", () => {
    const out = html("看日志：\n```\n**literal**\n  indented\n```");

    expect(out).toContain("<pre");
    expect(out).toContain("**literal**");
    expect(out).toContain("  indented");
    expect(out).not.toContain("<strong>");
    expect(out).not.toContain("```");
  });

  it("renders a quote and a rule as themselves", () => {
    const quote = html("> 一句引用");
    expect(quote).toContain("border-l-2");
    expect(quote).toContain("一句引用");

    const rule = html("---");
    expect(rule).toContain("<hr");
    expect(rule).not.toContain("---");
  });

  it("still renders bold and numbered items without showing their markers", () => {
    const out = html("**短链接**是核心\n\n3. 第三步");

    expect(out).toContain("<strong");
    expect(out).toContain("第三步");
    expect(out).not.toContain("*");
  });
});
