import { describe, expect, it } from "vitest";
import type { Span } from "./aiAnswer";
import { parseAnswer } from "./aiAnswer";

const spans = (text: string): Span[] => parseAnswer(text)[0].spans;

describe("parseAnswer", () => {
  it("turns **bold** into a bold span instead of literal asterisks", () => {
    expect(spans("**短链接**是核心")).toEqual([
      { kind: "bold", text: "短链接" },
      { kind: "text", text: "是核心" },
    ]);
  });

  it("keeps an unclosed ** bold while the answer is still streaming", () => {
    expect(spans("正在**生成")).toEqual([
      { kind: "text", text: "正在" },
      { kind: "bold", text: "生成" },
    ]);
  });

  it("keeps a lone marker as text rather than dropping it", () => {
    expect(spans("结尾的 **")).toEqual([{ kind: "text", text: "结尾的 **" }]);
    expect(spans("*强调*")).toEqual([{ kind: "text", text: "*强调*" }]);
    expect(spans("反引号 ` 单独出现")).toEqual([
      { kind: "text", text: "反引号 ` 单独出现" },
    ]);
  });

  it("parses inline code", () => {
    expect(spans("用 `docker logs` 看日志")).toEqual([
      { kind: "text", text: "用 " },
      { kind: "code", text: "docker logs" },
      { kind: "text", text: " 看日志" },
    ]);
  });

  it("classifies headings, bullets and numbered items", () => {
    expect(parseAnswer("### 小结")).toEqual([
      { kind: "heading", marker: "", spans: [{ kind: "text", text: "小结" }] },
    ]);
    expect(parseAnswer("- 生成短链")).toEqual([
      { kind: "bullet", marker: "", spans: [{ kind: "text", text: "生成短链" }] },
    ]);
    expect(parseAnswer("* 查询统计")).toEqual([
      { kind: "bullet", marker: "", spans: [{ kind: "text", text: "查询统计" }] },
    ]);
    expect(parseAnswer("3. 第三步")).toEqual([
      { kind: "ordered", marker: "3.", spans: [{ kind: "text", text: "第三步" }] },
    ]);
  });

  it("keeps blank lines so paragraphs stay separated", () => {
    const lines = parseAnswer("第一段\n\n第二段");
    expect(lines.map((line) => line.spans.length)).toEqual([1, 0, 1]);
  });

  it("leaves an answer with no markers as one text span per line", () => {
    expect(parseAnswer("就一句话")).toEqual([
      { kind: "text", marker: "", spans: [{ kind: "text", text: "就一句话" }] },
    ]);
  });

  it("renders a realistic answer without leaving an asterisk on screen", () => {
    const answer = [
      "我**没有找到相关资料**。",
      "",
      "你可以：",
      "- 直接说明你想了解的内容",
      "- 把相关资料直接发给我",
      "",
      "**需要我帮你查一下账号的短链总览数据吗？**",
    ].join("\n");

    const rendered = parseAnswer(answer).flatMap((line) => line.spans.map((s) => s.text));
    expect(rendered.join("")).not.toContain("*");
    expect(rendered).toContain("没有找到相关资料");
    expect(rendered).toContain("需要我帮你查一下账号的短链总览数据吗？");
  });

  it("parses a table into a header and its rows", () => {
    const answer = [
      "| 组件 | 技术 |",
      "| --- | --- |",
      "| Go 后端 | Gin + GORM |",
      "| 前端 | Vite |",
    ].join("\n");

    const cell = (text: string): Span[] => [{ kind: "text", text }];

    expect(parseAnswer(answer)).toEqual([
      {
        kind: "table",
        marker: "",
        spans: [],
        table: {
          head: [cell("组件"), cell("技术")],
          rows: [[cell("Go 后端"), cell("Gin + GORM")], [cell("前端"), cell("Vite")]],
        },
      },
    ]);
  });

  it("styles what is inside a cell", () => {
    const lines = parseAnswer("| **粗体** | `code` |\n| --- | --- |");

    expect(lines[0].table?.head).toEqual([
      [{ kind: "bold", text: "粗体" }],
      [{ kind: "code", text: "code" }],
    ]);
  });

  it("leaves pipes alone when no |---| line follows them", () => {
    const prose = parseAnswer("他说 | 这个符号 | 是竖线");
    expect(prose[0].kind).toBe("text");
    expect(prose[0].table).toBeUndefined();

    // Fenced by pipes but with no delimiter row: still not a table.
    const fenced = parseAnswer("| 这行 | 只是文字 |");
    expect(fenced[0].kind).toBe("text");
    expect(fenced[0].table).toBeUndefined();
  });

  it("renders a header whose rows have not streamed in yet", () => {
    const lines = parseAnswer("| 组件 | 技术 |\n| --- | --- |");

    expect(lines[0].kind).toBe("table");
    expect(lines[0].table?.rows).toEqual([]);
  });

  it("keeps a table from swallowing the paragraph after it", () => {
    const lines = parseAnswer("| a | b |\n| --- | --- |\n| 1 | 2 |\n\n说明文字");

    expect(lines.map((line) => line.kind)).toEqual(["table", "text", "text"]);
    expect(lines[0].table?.rows).toHaveLength(1);
    expect(lines[2].spans).toEqual([{ kind: "text", text: "说明文字" }]);
  });

  it("parses a fenced code block and drops its markers", () => {
    const lines = parseAnswer("说明\n```bash\n  ls -l\n```\n结束");

    expect(lines.map((line) => line.kind)).toEqual(["text", "code", "text"]);
    // The body keeps its indentation and is not styled: inside a fence, markers are text.
    expect(lines[1].spans).toEqual([{ kind: "text", text: "  ls -l" }]);
    expect(lines[2].spans).toEqual([{ kind: "text", text: "结束" }]);
  });

  it("leaves the inside of a fence alone", () => {
    const lines = parseAnswer("```\n**not bold**\n```");

    expect(lines[0].spans).toEqual([{ kind: "text", text: "**not bold**" }]);
  });

  it("takes an unclosed fence as the rest of the answer, mid-stream", () => {
    const lines = parseAnswer("```\nkada-api\n还没闭合");

    expect(lines).toHaveLength(1);
    expect(lines[0].kind).toBe("code");
    expect(lines[0].spans).toEqual([{ kind: "text", text: "kada-api\n还没闭合" }]);
  });

  it("classifies a horizontal rule and a quote", () => {
    expect(parseAnswer("---")[0].kind).toBe("rule");
    expect(parseAnswer("***")[0].kind).toBe("rule");
    // A bullet is still a bullet: it is the space after the marker that separates the two.
    expect(parseAnswer("- 一条列表").map((line) => line.kind)).toEqual(["bullet"]);
    expect(parseAnswer("> 一句引用")).toEqual([
      { kind: "quote", marker: "", spans: [{ kind: "text", text: "一句引用" }] },
    ]);
  });
});
