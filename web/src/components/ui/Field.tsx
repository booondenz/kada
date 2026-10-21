import type { ReactNode } from "react";
import { cn } from "@/lib/utils";

/**
 * A labelled form control: label, the control, and one line under it.
 *
 * The line under it is either an error or a hint, never both - an error replaces the hint because the
 * thing the hint was preparing the user for has just happened, and two lines of small type under one
 * input is how a form starts to look like a form to be debugged.
 *
 * This replaces the dark-only `auth/FormField`: the field is themed now, so the sign-in page can be the
 * same page as the rest of the app instead of a black poster with white inputs on it.
 */
export function Field({
  id,
  label,
  hint,
  error,
  children,
}: {
  id: string;
  label: string;
  hint?: string;
  error?: string;
  children?: ReactNode;
}) {
  return (
    <div>
      <label htmlFor={id} className="mb-1.5 block text-sm font-medium text-body">
        {label}
      </label>
      {children}
      {error ? (
        <p className="mt-1.5 text-xs text-danger">{error}</p>
      ) : hint ? (
        <p className="mt-1.5 text-xs text-muted">{hint}</p>
      ) : null}
    </div>
  );
}

const INPUT_BASE =
  "h-9 w-full rounded-md border bg-canvas px-3 text-sm text-strong placeholder:text-faint " +
  "transition focus:outline-none focus:ring-2";

/**
 * The input's border and focus ring, with the error state as an argument rather than a second set of
 * strings per call site.
 *
 * `bg-canvas` rather than a white wash: the field sits on the card, so it has to be the page surface, and
 * on a near-black page a translucent white is what turns a form into a glass panel.
 */
export function inputClass(invalid = false, className?: string): string {
  return cn(
    INPUT_BASE,
    invalid
      ? "border-danger focus:border-danger focus:ring-danger/20"
      : "border-line-strong focus:border-brand focus:ring-brand/20",
    className,
  );
}
