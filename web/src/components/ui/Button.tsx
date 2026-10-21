import type { ButtonHTMLAttributes } from "react";
import { cn } from "@/lib/utils";

/**
 * The one button in the app.
 *
 * Three variants, two heights, and nothing else: no shadow, no gradient, no `rounded-xl`. A control is
 * told apart from the page by its fill or its border, and that difference survives both themes; depth does
 * not, because a shadow that separates two light surfaces does nothing on a near-black one and has to be
 * reinvented at each call site.
 *
 * `primary` is the brand pair - `bg-brand text-white` - and stays fixed in both themes on purpose (see the
 * note in `globals.css`): brightening the fill for a dark surface hands white type a pale blue background.
 * `secondary` and `ghost` read through the theme tokens, so they are the same control in either theme.
 *
 * `buttonClass` is exported next to the component because react-router's `<Link>` renders an anchor, and
 * the one thing an anchor may not contain is a button. A link that has to look like the primary action
 * borrows these classes rather than wrapping one.
 */
export type ButtonVariant = "primary" | "secondary" | "ghost";
export type ButtonSize = "sm" | "md";

const VARIANT: Record<ButtonVariant, string> = {
  primary: "bg-brand text-white hover:bg-brand-dark",
  secondary: "border border-line-strong bg-canvas text-body hover:bg-muted-surface hover:text-strong",
  ghost: "text-muted hover:bg-muted-surface hover:text-strong",
};

/**
 * Heights, not padding. A row of controls has to line up with the 32px and 36px elements around it, and
 * `py-2.5` resolves to whatever the line box happens to be, which is how two buttons in the same toolbar
 * end up a pixel apart.
 */
const SIZE: Record<ButtonSize, string> = {
  sm: "h-8 px-3 text-xs",
  md: "h-9 px-4 text-sm",
};

const BASE =
  "inline-flex shrink-0 items-center justify-center gap-1.5 whitespace-nowrap rounded-md font-medium " +
  "transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand/40 " +
  "disabled:cursor-not-allowed disabled:opacity-50";

export function buttonClass({
  variant = "primary",
  size = "md",
  className,
}: { variant?: ButtonVariant; size?: ButtonSize; className?: string } = {}): string {
  return cn(BASE, VARIANT[variant], SIZE[size], className);
}

export function Button({
  variant = "primary",
  size = "md",
  className,
  type = "button",
  ...rest
}: ButtonHTMLAttributes<HTMLButtonElement> & { variant?: ButtonVariant; size?: ButtonSize }) {
  return <button type={type} className={buttonClass({ variant, size, className })} {...rest} />;
}
