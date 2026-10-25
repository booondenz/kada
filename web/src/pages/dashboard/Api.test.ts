import { describe, expect, it } from "vitest";
import { LANGUAGES } from "./Api";

// The examples are documentation, and the failure worth a test is a language tab that shows a snippet for
// the wrong API or loses the token. Each one must carry the caller's own origin and the placeholder the
// prose tells them to replace.
const BASE = "https://example.test";

describe("ApiPage examples", () => {
  it("offers the languages the API is called from, in one row", () => {
    expect(LANGUAGES.map((language) => language.label)).toEqual([
      "cURL",
      "Go",
      "C",
      "C++",
      "Java",
      "Python",
      "Rust",
      "JavaScript",
      "TypeScript",
      "C#",
    ]);
  });

  it("names the caller's own origin in every example", () => {
    for (const language of LANGUAGES) {
      expect(language.example(BASE)).toContain(`${BASE}/api/links`);
    }
  });

  it("carries the token placeholder and leaves no interpolation behind", () => {
    for (const language of LANGUAGES) {
      const example = language.example(BASE);
      expect(example).toContain("kada_your_token");
      expect(example).not.toContain("${base}");
    }
  });
});
