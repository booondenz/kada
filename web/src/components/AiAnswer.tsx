import type { Span, Table } from "@/lib/aiAnswer";
import { parseAnswer } from "@/lib/aiAnswer";

function Inline({ spans }: { spans: Span[] }) {
  return (
    <>
      {spans.map((span, i) => {
        if (span.kind === "bold") {
          return (
            <strong key={i} className="font-semibold">
              {span.text}
            </strong>
          );
        }
        if (span.kind === "code") {
          return (
            <code
              key={i}
              className="rounded bg-canvas px-1 py-0.5 font-mono text-[0.85em]"
            >
              {span.text}
            </code>
          );
        }
        return <span key={i}>{span.text}</span>;
      })}
    </>
  );
}

/**
 * A table the model wrote.
 *
 * Body cells are laid out from the header's cells rather than from their own row, so a row that arrived
 * with fewer cells than the header still gets a cell - an empty one - instead of a gap in the grid. A
 * wide table scrolls sideways; it never stretches the bubble.
 */
function TableBlock({ table }: { table: Table }) {
  return (
    <div className="my-1 overflow-x-auto">
      <table className="w-full border-collapse text-left text-[0.95em]">
        <thead className="bg-subtle">
          <tr>
            {table.head.map((cell, i) => (
              <th key={i} className="border border-line px-2 py-1 font-semibold">
                <Inline spans={cell} />
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {table.rows.map((row, r) => (
            <tr key={r}>
              {table.head.map((_, c) => (
                <td key={c} className="border border-line px-2 py-1 align-top">
                  <Inline spans={row[c] ?? []} />
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

/**
 * Renders one assistant answer.
 *
 * Only the assistant's text goes through here: what the user typed is shown
 * verbatim. See `lib/aiAnswer.ts` for what is parsed and why.
 */
export function AiAnswer({ text }: { text: string }) {
  return (
    <>
      {parseAnswer(text).map((line, i) => {
        // A code block and a table carry no spans, so both are answered before the blank-line case below.
        if (line.kind === "code") {
          return (
            <pre
              key={i}
              className="my-1 overflow-x-auto rounded-md bg-subtle p-2 font-mono text-[0.85em] leading-relaxed"
            >
              <code>{line.spans[0]?.text ?? ""}</code>
            </pre>
          );
        }
        if (line.kind === "table" && line.table) {
          return <TableBlock key={i} table={line.table} />;
        }
        if (line.kind === "rule") {
          return <hr key={i} className="my-2 border-t border-line" />;
        }
        if (line.spans.length === 0) {
          return <div key={i} className="h-2" />;
        }
        if (line.kind === "quote") {
          return (
            <div key={i} className="border-l-2 border-line pl-2 text-muted">
              <Inline spans={line.spans} />
            </div>
          );
        }
        if (line.kind === "bullet") {
          return (
            <div key={i} className="flex gap-1.5">
              <span aria-hidden className="text-faint">
                •
              </span>
              <span className="min-w-0 flex-1">
                <Inline spans={line.spans} />
              </span>
            </div>
          );
        }
        if (line.kind === "ordered") {
          return (
            <div key={i} className="flex gap-1.5">
              <span className="text-muted tabular-nums">{line.marker}</span>
              <span className="min-w-0 flex-1">
                <Inline spans={line.spans} />
              </span>
            </div>
          );
        }
        return (
          <div key={i} className={line.kind === "heading" ? "font-semibold" : undefined}>
            <Inline spans={line.spans} />
          </div>
        );
      })}
    </>
  );
}
