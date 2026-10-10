import { useState } from "react";
import { Eye, EyeOff } from "lucide-react";
import { useT } from "@/lib/i18n";

interface PasswordInputProps extends React.InputHTMLAttributes<HTMLInputElement> {
  id: string;
}

export function PasswordInput({ id, className = "", ...props }: PasswordInputProps) {
  const t = useT();
  const [show, setShow] = useState(false);
  return (
    <div className="relative">
      <input
        id={id}
        type={show ? "text" : "password"}
        className={`w-full rounded-lg border bg-white/10 py-2.5 pl-4 pr-11 text-sm text-white placeholder:text-neutral-400/70 transition focus:outline-none focus:ring-2 ${className}`}
        {...props}
      />
      <button
        type="button"
        onClick={() => setShow((s) => !s)}
        className="absolute inset-y-0 right-0 flex items-center pr-3 text-neutral-400 transition hover:text-neutral-200"
        tabIndex={-1}
        aria-label={show ? t("隐藏密码") : t("显示密码")}
      >
        {show ? <EyeOff className="size-4" /> : <Eye className="size-4" />}
      </button>
    </div>
  );
}
