import { Link } from "react-router";
import { Link2 } from "lucide-react";
import StarfieldCanvas from "@/components/StarfieldCanvas";
import { TopBarControls } from "@/components/TopBarControls";
import { TOP_BAR_GUTTER, TOP_BAR_HEIGHT } from "@/components/topBar";
import { useT } from "@/lib/i18n";

export default function HomePage() {
  const t = useT();

  return (
    <main className="relative h-dvh w-full overflow-hidden overscroll-none bg-deep-space text-white">
      {/* canvas is a replaced element, so inset-0 does not stretch it; h-full w-full is required to fill the viewport */}
      <StarfieldCanvas className="absolute inset-0 h-full w-full" />

      <div className="absolute inset-0 z-10 flex flex-col">
        {/* Height and gutter are the app shell's, because the language capsule is centred in this row: the
            row height is what decides which pixel it lands on. On the hero's own py-5 rhythm it sat 10px
            lower and 8px further from the edge than the capsule on the sign-in and dashboard screens. Only
            the geometry is shared - no border and no background, since this header floats on the starfield. */}
        <header className={`flex ${TOP_BAR_HEIGHT} items-center justify-between ${TOP_BAR_GUTTER}`}>
          <div className="flex items-center gap-2.5">
            <div className="flex size-8 items-center justify-center rounded-lg bg-indigo-600">
              <Link2 className="size-4 text-white" />
            </div>
            <span className="text-lg font-semibold tracking-wider">KADA</span>
          </div>
          <div className="flex items-center gap-2">
            {/* One call to action, not two: a phone number that has never been seen is registered on the
                spot, so "sign in" and "sign up" are the same page. Two buttons pointing at /login with
                different labels would only suggest a distinction that no longer exists. */}
            <Link
              to="/login"
              className="rounded-lg bg-indigo-600 px-4 py-2 text-sm font-medium text-white transition hover:bg-indigo-500"
            >
              {t("登录 / 注册")}
            </Link>
            <TopBarControls onDark />
          </div>
        </header>

        <div className="flex flex-1 flex-col items-center justify-center px-4 text-center">
          <h1 className="text-6xl font-black tracking-[0.18em] text-white drop-shadow-[0_0_28px_rgba(99,102,241,0.45)] sm:text-8xl">
            KADA
          </h1>
          <p className="mt-6 text-base font-medium tracking-[0.5em] text-indigo-200/90 sm:text-lg">
            {t("短链接平台")}
          </p>
        </div>
      </div>
    </main>
  );
}
