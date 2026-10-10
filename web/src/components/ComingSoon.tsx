import { type LucideIcon, Construction } from "lucide-react";

interface ComingSoonProps {
  title: string;
  description?: string;
  icon?: LucideIcon;
}

export function ComingSoon({ title, description, icon: Icon }: ComingSoonProps) {
  return (
    <div className="flex flex-col items-center justify-center py-24 text-center">
      <div className="flex size-16 items-center justify-center rounded-2xl bg-muted-surface mb-5">
        {Icon ? (
          <Icon className="size-8 text-faint" />
        ) : (
          <Construction className="size-8 text-faint" />
        )}
      </div>
      <h2 className="text-xl font-bold text-strong">{title}</h2>
      {description && (
        <p className="mt-2 text-sm text-muted max-w-sm">{description}</p>
      )}
    </div>
  );
}
