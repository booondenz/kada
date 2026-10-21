import { Link } from "react-router";
import { IcpNotice } from "@/components/IcpNotice";
import { TopBarControls } from "@/components/TopBarControls";
import { TOP_BAR_GUTTER, TOP_BAR_HEIGHT } from "@/components/topBar";
import { buttonClass } from "@/components/ui/Button";
import { useT } from "@/lib/i18n";

/**
 * The front page: what the product is, and one way in.
 *
 * This used to be a three.js starfield - 1,740 points, a constellation, two nebula sprites and a mouse
 * parallax - behind a glowing 8xl wordmark. It was the heaviest page on the site and it was carrying a
 * 3D engine into every other route, because `router.tsx` imports the pages statically. What replaced it
 * is the type: one heading at 30/36px, one sentence saying what the product does, and one button.
 *
 * The sentence is the part that matters. A page this quiet has to answer "what is this" before the
 * visitor has to guess, or "minimal" reads as "unfinished" - so the heading is the product, the line
 * under it is the mechanism, and there is nothing else on the page to compete with either.
 *
 * The header keeps the app shell's height and gutter, because the language capsule is centred in this
 * row and has to land on the same pixel it lands on in the dashboard. Only the geometry is shared: the
 * bar here has no border and no background, since the page is already the surface.
 */
export default function HomePage() {
  const t = useT();

  return (
    <div className="flex min-h-dvh flex-col bg-canvas">
      <header className={`flex ${TOP_BAR_HEIGHT} items-center justify-between ${TOP_BAR_GUTTER}`}>
        <span className="text-sm font-semibold tracking-tight text-strong">Kada</span>
        {/* One call to action, not two: a phone number that has never been seen is registered on the
            spot, so "sign in" and "sign up" are the same page. Two buttons pointing at /login with
            different labels would only suggest a distinction that no longer exists. */}
        <TopBarControls />
      </header>

      <main className="flex flex-1 items-center px-6 sm:px-8">
        <div className="mx-auto w-full max-w-xl">
          <h1 className="text-3xl font-semibold tracking-tight text-strong sm:text-4xl">
            {t("短链接与点击分析")}
          </h1>
          <p className="mt-4 max-w-md text-base leading-relaxed text-muted">
            {t("把长链接变成短链接，然后在同一个地方看清谁点了、从哪里来。")}
          </p>
          <div className="mt-8 flex items-center gap-5">
            <Link to="/login" className={buttonClass()}>
              {t("开始使用")}
            </Link>
            <Link
              to="/privacy"
              className="text-sm text-muted underline-offset-4 transition-colors hover:text-strong hover:underline"
            >
              {t("隐私政策")}
            </Link>
          </div>
        </div>
      </main>

      {/* The filing number goes at the bottom of the homepage, where the regulator expects to find it:
          quiet, on the baseline of the page, in the same column as the type above it. */}
      <footer className="px-6 pb-6 sm:px-8">
        <div className="mx-auto w-full max-w-xl">
          <IcpNotice className="text-xs text-faint transition-colors hover:text-muted" />
        </div>
      </footer>
    </div>
  );
}
