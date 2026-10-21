import type { ReactNode } from "react";

/**
 * The title block at the top of a page: one heading, an optional line under it, and the page's own
 * actions on the right.
 *
 * It exists because the pages were each writing `<h1 className="text-2xl font-bold">` plus their own
 * spacing, so the same block was 20px on one page and 24px on another and the toolbar below it moved with
 * it. Sizes and gaps come from here now, and the twelve pages render the same shape.
 */
export function PageHeader({
  title,
  description,
  actions,
}: {
  title: string;
  description?: string;
  actions?: ReactNode;
}) {
  return (
    <div className="mb-6 flex items-start justify-between gap-4">
      <div className="min-w-0">
        <h1 className="text-xl font-semibold tracking-tight text-strong">{title}</h1>
        {description && <p className="mt-1 text-sm text-muted">{description}</p>}
      </div>
      {actions && <div className="flex shrink-0 items-center gap-2">{actions}</div>}
    </div>
  );
}
