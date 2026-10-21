import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { Button, buttonClass } from "./Button";
import { Card } from "./Card";
import { Field, inputClass } from "./Field";
import { PageHeader } from "./PageHeader";

// The primitives are where the visual rules are enforced, because a page cannot drift from a component it
// does not write. These assertions are the rules themselves - one accent, hairlines instead of shadows,
// one radius, a fixed height - so a later "let's make this card pop" has to break a test to land.
describe("buttonClass", () => {
  it("draws the primary action from the brand pair, which is fixed in both themes", () => {
    expect(buttonClass()).toContain("bg-brand");
    expect(buttonClass()).toContain("text-white");
  });

  it("gives every variant a fixed height rather than padding, so a row of controls lines up", () => {
    expect(buttonClass({ size: "md" })).toContain("h-9");
    expect(buttonClass({ size: "sm" })).toContain("h-8");
    expect(buttonClass({ size: "md" })).not.toContain("py-");
  });

  it("keeps the caller's own classes last, so a page can widen a button", () => {
    expect(buttonClass({ className: "w-full" })).toMatch(/w-full$/);
  });
});

describe("Button", () => {
  it("is a plain button unless it is told otherwise", () => {
    expect(renderToStaticMarkup(<Button>保存</Button>)).toContain('type="button"');
  });

  it("stays a submit button when the form asks for one", () => {
    expect(renderToStaticMarkup(<Button type="submit">登录</Button>)).toContain('type="submit"');
  });
});

describe("Card", () => {
  it("separates by a hairline rather than by depth", () => {
    const html = renderToStaticMarkup(<Card>内容</Card>);

    expect(html).toContain("border-line");
    expect(html).not.toContain("shadow");
  });

  it("uses the app's one radius", () => {
    expect(renderToStaticMarkup(<Card />)).toContain("rounded-md");
  });
});

describe("PageHeader", () => {
  it("renders the title and the line under it", () => {
    const html = renderToStaticMarkup(<PageHeader title="数据统计" description="链接点击数据和概览" />);

    expect(html).toContain("数据统计");
    expect(html).toContain("链接点击数据和概览");
  });

  it("renders the page's actions beside the title, not below it", () => {
    const html = renderToStaticMarkup(<PageHeader title="链接" actions={<Button>新建</Button>} />);

    expect(html).toContain("新建");
  });
});

describe("inputClass", () => {
  it("uses the control border, and the accent only on focus", () => {
    const normal = inputClass();

    expect(normal).toContain("border-line-strong");
    expect(normal).toContain("focus:border-brand");
  });

  it("swaps the border for the danger token when the field is invalid", () => {
    const invalid = inputClass(true);

    expect(invalid).toContain("border-danger");
    expect(invalid).not.toContain("focus:border-brand");
  });
});

describe("Field", () => {
  it("labels the control it wraps", () => {
    expect(renderToStaticMarkup(<Field id="phone" label="手机号" />)).toContain('for="phone"');
  });

  it("shows the error in place of the hint, never both", () => {
    const html = renderToStaticMarkup(
      <Field id="password" label="密码" hint="至少 8 位字符" error="密码至少 8 位" />,
    );

    expect(html).toContain("密码至少 8 位");
    expect(html).not.toContain("至少 8 位字符");
    expect(html).toContain("text-danger");
  });
});
