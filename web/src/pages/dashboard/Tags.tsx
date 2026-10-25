import { useState } from "react";
import { Tag, Plus, Trash2, Check, X } from "lucide-react";
import useSWR from "swr";
import { toast } from "sonner";
import { tagsAPI } from "@/lib/api";
import { getToken } from "@/lib/auth";
import { useT } from "@/lib/i18n";
import { Button } from "@/components/ui/Button";
import { PageHeader } from "@/components/ui/PageHeader";

const TAG_COLORS = [
  "#3B82F6", // blue
  "#10B981", // emerald
  "#F59E0B", // amber
  "#EF4444", // red
  "#8B5CF6", // violet
  "#EC4899", // pink
  "#06B6D4", // cyan
  "#F97316", // orange
];

interface TagItem {
  id: number;
  name: string;
  color: string;
  link_count?: number;
}

export default function TagsPage() {
  const token = getToken();
  const t = useT();

  const { data, error, isLoading, mutate } = useSWR(
    token ? "tags" : null,
    () => tagsAPI.list(token!)
  );

  const tags: TagItem[] = data?.tags ?? [];

  const [newName, setNewName] = useState("");
  const [newColor, setNewColor] = useState(TAG_COLORS[0]);
  const [creating, setCreating] = useState(false);
  const [deletingId, setDeletingId] = useState<number | null>(null);

  const handleCreate = async () => {
    if (!token || !newName.trim()) return;
    setCreating(true);
    try {
      await tagsAPI.create(token, newName.trim(), newColor);
      setNewName("");
      setNewColor(TAG_COLORS[0]);
      mutate();
      toast.success(t("标签已创建"));
    } catch {
      toast.error(t("创建失败"));
    } finally {
      setCreating(false);
    }
  };

  const handleDelete = async (id: number) => {
    if (!token) return;
    try {
      await tagsAPI.delete(token, id);
      setDeletingId(null);
      mutate();
      toast.success(t("已删除"));
    } catch {
      toast.error(t("删除失败"));
    }
  };

  return (
    <div className="space-y-5">
      <PageHeader title={t("标签")} description={t("用标签标记和筛选链接")} />

      {/* Create new tag */}
      <div className="rounded-xl border border-line bg-canvas shadow-sm p-4">
        <form
          onSubmit={(e) => {
            e.preventDefault();
            handleCreate();
          }}
          className="flex items-center gap-3 flex-wrap"
        >
          <div
            className="flex size-9 items-center justify-center rounded-lg shrink-0"
            style={{ backgroundColor: newColor + "18" }}
          >
            <Tag className="size-4" style={{ color: newColor }} />
          </div>
          <input
            type="text"
            value={newName}
            onChange={(e) => setNewName(e.target.value)}
            placeholder={t("新建标签...")}
            className="flex-1 min-w-[140px] border-none bg-transparent text-sm text-strong placeholder:text-faint focus:outline-none"
            maxLength={30}
          />

          {/* Color selector */}
          <div className="flex items-center gap-1.5">
            {TAG_COLORS.map((c) => (
              <button
                key={c}
                type="button"
                onClick={() => setNewColor(c)}
                className={`size-5 rounded-full transition-all ${
                  c === newColor
                    ? "scale-110"
                    : "hover:scale-105"
                }`}
                style={{
                  backgroundColor: c,
                  ...(c === newColor
                    ? { boxShadow: `0 0 0 2px white, 0 0 0 4px ${c}` }
                    : {}),
                }}
              />
            ))}
          </div>

          <Button type="submit" size="sm" disabled={!newName.trim() || creating}>
            <Plus className="size-3.5" />
            {t("新建")}
          </Button>
        </form>
      </div>

      {/* One sentence and a reload, as on every list page: the emoji, the heading and the paragraph are
          gone, because a failed request does not need a layout. */}
      {error ? (
        <div className="py-16 text-center">
          <p className="text-sm text-muted">{t("加载失败，请检查网络后重试。")}</p>
          <button
            onClick={() => mutate()}
            className="mt-3 text-sm font-medium text-brand-ink hover:text-brand"
          >
            {t("重新加载")}
          </button>
        </div>
      ) : isLoading ? (
        /* Skeleton */
        <div className="flex flex-wrap gap-2">
          {Array.from({ length: 6 }).map((_, i) => (
            <div
              key={i}
              className="animate-pulse rounded-full h-8 w-20 bg-muted-surface"
            />
          ))}
        </div>
      ) : tags.length === 0 ? (
        /* Empty state: one line, because the creating happens in the form above. */
        <p className="py-16 text-center text-sm text-muted">{t("还没有标签")}</p>
      ) : (
        /* Tag list - pill style */
        <div className="flex flex-wrap gap-2">
          {tags.map((tag) => (
            <div
              key={tag.id}
              className="group inline-flex items-center gap-1.5 rounded-full px-3 py-1.5 text-sm font-medium transition"
              style={{
                backgroundColor: (tag.color || "#3B82F6") + "15",
                color: tag.color || "#3B82F6",
              }}
            >
              {tag.name}
              <span className="text-xs opacity-60">
                {tag.link_count ?? 0}
              </span>

              {deletingId === tag.id ? (
                <span className="inline-flex items-center gap-1 ml-1">
                  <button
                    onClick={() => handleDelete(tag.id)}
                    className="hover:opacity-80 transition"
                    title={t("确认删除")}
                  >
                    <Check className="size-3" />
                  </button>
                  <button
                    onClick={() => setDeletingId(null)}
                    className="hover:opacity-80 transition"
                    title={t("取消")}
                  >
                    <X className="size-3" />
                  </button>
                </span>
              ) : (
                <button
                  onClick={() => setDeletingId(tag.id)}
                  className="opacity-0 group-hover:opacity-100 hover:opacity-80 transition ml-1"
                  title={t("删除标签")}
                >
                  <Trash2 className="size-3" />
                </button>
              )}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
