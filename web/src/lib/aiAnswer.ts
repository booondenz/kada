// The assistant answers in Markdown, but the chat bubble renders plain text, so
// every `**bold**` in an answer reached the user as literal asterisks. This
// module parses the subset the model actually produces - bold, inline code,
// headings, bullets, numbered items, tables, fenced code blocks, quotes and
// horizontal rules - so the component can render it. A full Markdown dependency
// would be far more machinery than that subset needs, and this keeps everything
// as text: nothing here can turn into HTML.
//
// It is also written for a token stream. While an answer is still arriving, an
// unclosed `**` renders as bold to the end of what has arrived so far, instead
// of flashing a stray asterisk and repairing itself on the next chunk. An
// unclosed ``` fence and a header row whose |---| line has not arrived yet are
// the same idea: show what is there rather than guessing at what is coming.

export type SpanKind = "text" | "bold" | "code";

export interface Span {
  kind: SpanKind;
  text: string;
}

export type LineKind =
  | "text"
  | "bullet"
  | "ordered"
  | "heading"
  | "quote"
  | "rule"
  | "code"
  | "table";

/** A table the model wrote: the header cells, then one entry per body row. */
export interface Table {
  head: Span[][];
  rows: Span[][][];
}

export interface Line {
  kind: LineKind;
  /** Original marker of an ordered item ("3."); empty for every other kind. */
  marker: string;
  /**
   * The styled runs of a line. Empty for a blank line, which is what separates paragraphs, and for a
   * table, whose text lives in `table` instead. A code block keeps its body here as one plain span:
   * nothing inside a fence is styled, which is the point of a fence.
   */
  spans: Span[];
  /** Set on a "table" line only. */
  table?: Table;
}

const HEADING = /^#{1,6}\s+/;
const BULLET = /^\s*[-*•]\s+/;
const ORDERED = /^\s*(\d{1,2})[.)]\s+/;
const QUOTE = /^\s*>\s?/;
const RULE = /^\s*(?:-{3,}|\*{3,}|_{3,})\s*$/;
/** A fence. Whatever follows it is the language, which the bubble does not label. */
const FENCE = /^\s*(```|~~~)/;
const TABLE_ROW = /^\s*\|.*\|\s*$/;
const TABLE_CELL = /^:?-+:?$/;
const BOLD_MARKER = "**";
const CODE_MARKER = "`";

/** Join neighbouring plain-text spans, so callers see "a **b** c" as three. */
function mergeText(spans: Span[]): Span[] {
  return spans.reduce<Span[]>((out, span) => {
    const last = out[out.length - 1];
    if (last?.kind === "text" && span.kind === "text") {
      last.text += span.text;
      return out;
    }
    out.push({ ...span });
    return out;
  }, []);
}

/**
 * Split one line into styled spans.
 *
 * An unclosed `**` styles the rest of the line: mid-stream that is the correct
 * reading, and once the closing marker arrives it is parsed normally. An
 * unclosed backtick stays literal, because a lone backtick is more likely to be
 * text than the start of inline code.
 */
function parseSpans(text: string): Span[] {
  const spans: Span[] = [];
  let rest = text;

  while (rest) {
    const boldAt = rest.indexOf(BOLD_MARKER);
    const codeAt = rest.indexOf(CODE_MARKER);

    let at = -1;
    let kind: SpanKind = "text";
    let marker = "";
    if (boldAt >= 0 && (codeAt < 0 || boldAt < codeAt)) {
      at = boldAt;
      kind = "bold";
      marker = BOLD_MARKER;
    } else if (codeAt >= 0) {
      at = codeAt;
      kind = "code";
      marker = CODE_MARKER;
    }

    if (at < 0) {
      spans.push({ kind: "text", text: rest });
      break;
    }
    if (at > 0) {
      spans.push({ kind: "text", text: rest.slice(0, at) });
    }

    const after = rest.slice(at + marker.length);
    const close = after.indexOf(marker);
    if (close < 0) {
      if (kind === "bold" && after) {
        spans.push({ kind: "bold", text: after });
      } else {
        // Nothing to style: keep the marker itself rather than dropping it.
        spans.push({ kind: "text", text: marker + after });
      }
      break;
    }

    spans.push({ kind, text: after.slice(0, close) });
    rest = after.slice(close + marker.length);
  }

  return mergeText(spans);
}

/**
 * The cells of a `| a | b |` row, or null when the line is not one.
 *
 * Both ends have to be fenced by a pipe. That is what the model writes, and it is also what keeps a
 * sentence that happens to contain a `|` from being read as a table: the row below it would have to be
 * a `|---|---|` line as well.
 */
function tableRow(line: string): string[] | null {
  if (!TABLE_ROW.test(line)) {
    return null;
  }
  return line
    .trim()
    .slice(1, -1)
    .split("|")
    .map((cell) => cell.trim());
}

/** Split an answer into one entry per line, blanks included. */
export function parseAnswer(text: string): Line[] {
  const raw = text.split("\n");
  const lines: Line[] = [];

  for (let i = 0; i < raw.length; i++) {
    const line = raw[i].trimEnd();

    const fence = line.match(FENCE);
    if (fence) {
      const marker = fence[1];
      const body: string[] = [];
      // An unclosed fence is the streaming case: everything that has arrived belongs to the block. When
      // there is a closing fence, the loop's own i++ skips it.
      for (i++; i < raw.length && !raw[i].trimEnd().startsWith(marker); i++) {
        body.push(raw[i]);
      }
      lines.push({ kind: "code", marker: "", spans: [{ kind: "text", text: body.join("\n") }] });
      continue;
    }

    // A table is the one construct that spans lines: the row below the header has to be a |---| line for
    // either of them to mean anything, and every following pipe row belongs to the same table.
    const head = tableRow(line);
    if (head) {
      const delimiter = i + 1 < raw.length ? tableRow(raw[i + 1].trimEnd()) : null;
      if (delimiter && delimiter.every((cell) => TABLE_CELL.test(cell))) {
        const rows: Span[][][] = [];
        let end = i + 2;
        for (; end < raw.length; end++) {
          const row = tableRow(raw[end].trimEnd());
          if (!row) {
            break;
          }
          rows.push(row.map(parseSpans));
        }
        lines.push({
          kind: "table",
          marker: "",
          spans: [],
          // The delimiter's cell count is not compared with the header's: a mismatch is a rendering
          // detail (the component lays rows out from the header), not a reason to show pipes to a reader.
          table: { head: head.map(parseSpans), rows },
        });
        i = end - 1;
        continue;
      }
    }

    if (RULE.test(line)) {
      lines.push({ kind: "rule", marker: "", spans: [] });
      continue;
    }

    const quote = line.match(QUOTE);
    if (quote) {
      lines.push({ kind: "quote", marker: "", spans: parseSpans(line.slice(quote[0].length)) });
      continue;
    }

    const heading = line.match(HEADING);
    if (heading) {
      lines.push({
        kind: "heading",
        marker: "",
        spans: parseSpans(line.slice(heading[0].length)),
      });
      continue;
    }

    const ordered = line.match(ORDERED);
    if (ordered) {
      lines.push({
        kind: "ordered",
        marker: `${ordered[1]}.`,
        spans: parseSpans(line.slice(ordered[0].length)),
      });
      continue;
    }

    const bullet = line.match(BULLET);
    if (bullet) {
      lines.push({ kind: "bullet", marker: "", spans: parseSpans(line.slice(bullet[0].length)) });
      continue;
    }

    lines.push({ kind: "text", marker: "", spans: parseSpans(line) });
  }

  return lines;
}
