import { useState } from "react";
import { Folder, Plus, Pencil, Trash2, Check, X, FolderOpen } from "lucide-react";
import useSWR from "swr";
import { toast } from "sonner";
import { foldersAPI } from "@/lib/api";
import { getToken } from "@/lib/auth";
import { useT } from "@/lib/i18n";

interface FolderItem {
  id: number;
  name: string;
  link_count: number;
}

export default function FoldersPage() {
  const token = getToken();
  const t = useT();

  const { data, error, isLoading, mutate } = useSWR(
    token ? "folders" : null,
    () => foldersAPI.list(token!)
  );

  const folders: FolderItem[] = data?.folders ?? [];

  const [newName, setNewName] = useState("");
  const [creating, setCreating] = useState(false);
  const [editingId, setEditingId] = useState<number | null>(null);
  const [editingName, setEditingName] = useState("");
  const [deletingId, setDeletingId] = useState<number | null>(null);

  const handleCreate = async () => {
    if (!token || !newName.trim()) return;
    setCreating(true);
    try {
      await foldersAPI.create(token, newName.trim());
      setNewName("");
      mutate();
      toast.success(t("文件夹已创建"));
    } catch {
      toast.error(t("创建失败"));
    } finally {
      setCreating(false);
    }
  };

  const handleRename = async (id: number) => {
    if (!token || !editingName.trim()) return;
    try {
      await foldersAPI.update(token, id, editingName.trim());
      setEditingId(null);
      setEditingName("");
      mutate();
      toast.success(t("已重命名"));
    } catch {
      toast.error(t("重命名失败"));
    }
  };

  const handleDelete = async (id: number) => {
    if (!token) return;
    try {
      await foldersAPI.delete(token, id);
      setDeletingId(null);
      mutate();
      toast.success(t("已删除"));
    } catch {
      toast.error(t("删除失败"));
    }
  };

  return (
    <div className="space-y-5">
      <div>
        <h1 className="text-2xl font-bold text-strong">{t("文件夹")}</h1>
        <p className="text-sm text-muted mt-1">{t("用文件夹组织你的短链接")}</p>
      </div>

      {/* Create new */}
      <div className="rounded-xl border border-line bg-canvas shadow-sm p-4">
        <form
          onSubmit={(e) => {
            e.preventDefault();
            handleCreate();
          }}
          className="flex items-center gap-3"
        >
          <div className="flex size-9 items-center justify-center rounded-lg bg-muted-surface shrink-0">
            <Folder className="size-4 text-muted" />
          </div>
          <input
            type="text"
            value={newName}
            onChange={(e) => setNewName(e.target.value)}
            placeholder={t("新建文件夹...")}
            className="flex-1 border-none bg-transparent text-sm text-strong placeholder:text-faint focus:outline-none"
            maxLength={50}
          />
          <button
            type="submit"
            disabled={!newName.trim() || creating}
            className="inline-flex items-center gap-1.5 rounded-lg bg-indigo-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-indigo-700 disabled:opacity-40 disabled:cursor-not-allowed transition shrink-0"
          >
            <Plus className="size-3.5" />
            {t("新建")}
          </button>
        </form>
      </div>

      {/* Error state */}
      {error ? (
        <div className="flex flex-col items-center justify-center py-16 text-center">
          <div className="text-3xl mb-3">😞</div>
          <h3 className="text-lg font-semibold text-strong">{t("加载失败")}</h3>
          <p className="mt-1 text-sm text-muted">{t("请检查网络后重试")}</p>
          <button
            onClick={() => mutate()}
            className="mt-3 text-sm font-medium text-brand-ink hover:text-brand"
          >
            {t("重新加载")}
          </button>
        </div>
      ) : isLoading ? (
        /* Skeleton */
        <div className="space-y-1.5">
          {Array.from({ length: 5 }).map((_, i) => (
            <div
              key={i}
              className="animate-pulse rounded-xl border border-line bg-canvas p-4 flex items-center gap-4"
            >
              <div className="size-9 rounded-lg bg-muted-surface" />
              <div className="flex-1 space-y-2">
                <div className="h-4 w-32 rounded bg-muted-surface" />
                <div className="h-3 w-16 rounded bg-gray-50" />
              </div>
            </div>
          ))}
        </div>
      ) : folders.length === 0 ? (
        /* Empty state */
        <div className="flex flex-col items-center justify-center py-16 text-center">
          <div className="flex size-14 items-center justify-center rounded-2xl bg-muted-surface mb-4">
            <FolderOpen className="size-7 text-faint" />
          </div>
          <h3 className="text-lg font-semibold text-strong">{t("还没有文件夹")}</h3>
          <p className="mt-1 text-sm text-muted">
            {t("在上方输入框输入名称来创建第一个文件夹")}
          </p>
        </div>
      ) : (
        /* Folder list */
        <div className="space-y-1.5">
          {folders.map((f) => (
            <div
              key={f.id}
              className="group rounded-xl border border-line bg-canvas p-4 flex items-center gap-4 hover:border-line hover:shadow-sm transition"
            >
              <div className="flex size-9 items-center justify-center rounded-lg bg-amber-50 shrink-0">
                <Folder className="size-4 text-amber-600" />
              </div>

              <div className="flex-1 min-w-0">
                {editingId === f.id ? (
                  <div className="flex items-center gap-2">
                    <input
                      type="text"
                      value={editingName}
                      onChange={(e) => setEditingName(e.target.value)}
                      className="border border-line rounded-lg px-2 py-1 text-sm text-strong focus:outline-none focus:ring-2 focus:ring-indigo-500 focus:border-transparent"
                      autoFocus
                      onKeyDown={(e) => {
                        if (e.key === "Enter") handleRename(f.id);
                        if (e.key === "Escape") {
                          setEditingId(null);
                          setEditingName("");
                        }
                      }}
                    />
                    <button
                      onClick={() => handleRename(f.id)}
                      className="p-1 rounded text-emerald-600 hover:bg-emerald-50 transition"
                    >
                      <Check className="size-3.5" />
                    </button>
                    <button
                      onClick={() => {
                        setEditingId(null);
                        setEditingName("");
                      }}
                      className="p-1 rounded text-faint hover:bg-muted-surface transition"
                    >
                      <X className="size-3.5" />
                    </button>
                  </div>
                ) : (
                  <>
                    <p
                      className="text-sm font-medium text-strong truncate cursor-pointer hover:text-brand-ink transition"
                      onClick={() => {
                        setEditingId(f.id);
                        setEditingName(f.name);
                      }}
                      title={t("点击编辑名称")}
                    >
                      {f.name}
                    </p>
                    <p className="text-xs text-faint mt-0.5">
                      {t("{f.link_count} 个链接", { "f.link_count": f.link_count })}
                    </p>
                  </>
                )}
              </div>

              <div className="flex items-center gap-1 opacity-0 group-hover:opacity-100 transition">
                <button
                  onClick={() => {
                    setEditingId(f.id);
                    setEditingName(f.name);
                  }}
                  className="p-1.5 rounded-lg text-faint hover:text-muted hover:bg-muted-surface transition"
                >
                  <Pencil className="size-3.5" />
                </button>

                {deletingId === f.id ? (
                  <div className="flex items-center gap-1.5 bg-red-50 px-2 py-1 rounded-lg">
                    <span className="text-xs text-red-600">{t("删除？")}</span>
                    <button
                      onClick={() => handleDelete(f.id)}
                      className="p-1 rounded text-red-600 hover:bg-red-100 transition"
                    >
                      <Check className="size-3" />
                    </button>
                    <button
                      onClick={() => setDeletingId(null)}
                      className="p-1 rounded text-faint hover:bg-raised transition"
                    >
                      <X className="size-3" />
                    </button>
                  </div>
                ) : (
                  <button
                    onClick={() => setDeletingId(f.id)}
                    className="p-1.5 rounded-lg text-faint hover:text-red-500 hover:bg-red-50 transition"
                  >
                    <Trash2 className="size-3.5" />
                  </button>
                )}
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
