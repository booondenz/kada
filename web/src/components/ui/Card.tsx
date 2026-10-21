import type { ComponentProps } from "react";
import { cn } from "@/lib/utils";

/**
 * The surface a group of things sits on: a hairline and a step of background, never a shadow.
 *
 * The radius is the app's one radius (6px), which is the difference between a page that looks drawn and
 * one that looks assembled - `rounded-lg` on the card, `rounded-xl` on the panel inside it and
 * `rounded-full` on its header is three decisions made by three different afternoons.
 *
 * Separation on its own is deliberate: elevation is reserved for things that genuinely float (a menu, a
 * dialog), so a page is a set of regions rather than a stack of sheets.
 */
export function Card({ className, ...rest }: ComponentProps<"div">) {
  return <div className={cn("rounded-md border border-line bg-canvas", className)} {...rest} />;
}
