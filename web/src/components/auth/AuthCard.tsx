import { Link2 } from "lucide-react";
import { IcpNotice } from "@/components/IcpNotice";
import { Card } from "@/components/ui/Card";

/**
 * The sign-in page's shell: a wordmark, the form's title, and the form itself on a card.
 *
 * It used to be a permanently dark page with a three.js starfield behind a glass panel, which made the one
 * screen every account passes through look like a different product from the one it leads into. The page
 * is themed now and follows the visitor's choice like every other page, and what is left is the type and
 * one hairline - which is the whole design, and the reason there is nothing here to explain.
 *
 * This is also where the filing number lives now. `/` has no landing page any more - it opens the
 * dashboard, which sends anyone without a session here - so the sign-in screen is the page a visitor sees
 * first, and the one a filing spot check sees. The number has to be on a page reachable without an
 * account, and `/privacy` carries it too.
 *
 * The footer slot keeps the mode switch ("no account yet? register"), because that switch is part of the
 * form rather than part of the brand.
 */
interface AuthCardProps {
  title: string;
  subtitle: string;
  footer: React.ReactNode;
  children: React.ReactNode;
}

export function AuthCard({ title, subtitle, footer, children }: AuthCardProps) {
  return (
    <div className="flex min-h-dvh flex-col bg-subtle">
      <div className="flex flex-1 items-center justify-center px-4 py-12">
        <div className="w-full max-w-sm">
          <div className="mb-8 flex flex-col items-center text-center">
            <div className="mb-4 flex size-9 items-center justify-center rounded-md bg-brand">
              <Link2 className="size-4 text-white" />
            </div>
            <h1 className="text-xl font-semibold tracking-tight text-strong">{title}</h1>
            <p className="mt-1.5 text-sm text-muted">{subtitle}</p>
          </div>
          <Card className="p-6 sm:p-7">{children}</Card>
          <p className="mt-6 text-center text-sm text-muted">{footer}</p>
        </div>
      </div>
      <footer className="px-6 pb-6">
        <div className="text-center">
          <IcpNotice className="text-xs text-faint transition-colors hover:text-muted" />
        </div>
      </footer>
    </div>
  );
}
