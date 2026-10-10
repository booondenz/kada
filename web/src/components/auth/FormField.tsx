interface FormFieldProps {
  id: string;
  label: string;
  error?: string;
  children: React.ReactNode;
}

export function FormField({ id, label, error, children }: FormFieldProps) {
  return (
    <div>
      <label htmlFor={id} className="mb-1.5 block text-sm font-medium text-neutral-100">
        {label}
      </label>
      {children}
      {error && <p className="mt-1.5 text-xs text-red-400">{error}</p>}
    </div>
  );
}

/** Base input styles (excluding border colour/focus state) — dark starfield theme */
export const inputBase =
  "w-full rounded-lg border bg-white/10 px-4 py-2.5 text-sm text-white placeholder:text-neutral-400/70 transition focus:outline-none focus:ring-2";

/** Input border/focus state: pass invalid=true to show the red error state */
export function fieldState(invalid: boolean): string {
  return invalid
    ? "border-red-400/60 focus:border-red-400 focus:ring-red-400/30"
    : "border-white/15 focus:border-indigo-400 focus:ring-indigo-400/30";
}
