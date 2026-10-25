import { useEffect, useRef, useState } from "react";
import { Link } from "react-router";
import { ChevronUp, ShieldCheck } from "lucide-react";
import { cn } from "@/lib/utils";
import { useT } from "@/lib/i18n";
import { IcpNotice } from "@/components/IcpNotice";
import { PublicSecurityNotice } from "@/components/PublicSecurityNotice";

/**
 * The two filings and the two links that belong with them, behind one control.
 *
 * The sidebar is 240px wide and the two numbers come to roughly 290px side by side, so as plain text they
 * take three rows at the bottom of every page, under a rule that marks the block off as legal text. One
 * button holds them instead and the panel expands in place - a floating popover would be clipped by the
 * sidebar's own scroll container - and the rule goes with the rows.
 *
 * The numbers stay in the document while the panel is closed (`hidden`, never removed from the tree). They
 * are a filing requirement rather than copy, and IcpNotice.test.tsx exists to catch the redesign that
 * deletes them: what changed is that they are one click away, not that they are gone. If a review ever
 * requires them on screen without interaction, this component is the single place that decides - the
 * privacy page already shows both without any interaction at all.
 */
export function FilingCredits() {
  const t = useT();
  const [open, setOpen] = useState(false);
  const root = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;

    const closeOnOutsideClick = (event: MouseEvent) => {
      if (!root.current?.contains(event.target as Node)) setOpen(false);
    };
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key === "Escape") setOpen(false);
    };

    document.addEventListener("mousedown", closeOnOutsideClick);
    document.addEventListener("keydown", closeOnEscape);
    return () => {
      document.removeEventListener("mousedown", closeOnOutsideClick);
      document.removeEventListener("keydown", closeOnEscape);
    };
  }, [open]);

  return (
    <div ref={root}>
      {/* Rendered above the trigger because the control sits at the bottom of the column: the panel grows
          upwards, into the space the navigation gives back. */}
      <div
        id="filing-credits"
        hidden={!open}
        className="mb-1 space-y-1.5 rounded-lg border border-line bg-canvas p-2.5"
      >
        <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs">
          <Link
            to="/privacy"
            onClick={() => setOpen(false)}
            className="text-muted transition-colors hover:text-strong"
          >
            {t("隐私政策")}
          </Link>
          <Link
            to="/report"
            onClick={() => setOpen(false)}
            className="text-muted transition-colors hover:text-strong"
          >
            {t("举报")}
          </Link>
        </div>
        <IcpNotice className="block text-faint transition-colors hover:text-muted" />
        <PublicSecurityNotice className="block text-faint transition-colors hover:text-muted" />
      </div>

      <button
        type="button"
        aria-expanded={open}
        aria-controls="filing-credits"
        onClick={() => setOpen((isOpen) => !isOpen)}
        className="flex w-full items-center justify-between rounded-lg px-1 py-1 text-xs text-faint transition-colors hover:text-muted"
      >
        <span className="flex items-center gap-1.5">
          <ShieldCheck className="size-3.5" />
          {t("备案与隐私")}
        </span>
        {/* The panel opens upwards, so the chevron points that way while it is closed and turns over once
            the panel is out. */}
        <ChevronUp
          className={cn("size-3.5 transition-transform", open && "rotate-180")}
          aria-hidden="true"
        />
      </button>
    </div>
  );
}
