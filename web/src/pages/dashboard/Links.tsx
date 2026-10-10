import { useState, useEffect, useRef } from "react";
import { Link } from "react-router";
import { Plus, Link2 } from "lucide-react";
import useSWR from "swr";
import { toast } from "sonner";
import { linksAPI, foldersAPI, tagsAPI, workspacesAPI } from "@/lib/api";
import { getToken } from "@/lib/auth";
import { LinkCard, LinkCardPlaceholder } from "@/components/LinkCard";
import { LinksToolbar } from "@/components/LinksToolbar";
import { useT } from "@/lib/i18n";
import type { LinkItem } from "@/components/LinkCard";

const PAGE_SIZE = 20;

export default function DashboardPage() {
  const t = useT();
  const token = getToken();
  const [page, setPage] = useState(1);
  const [search, setSearch] = useState("");
  const [searchInput, setSearchInput] = useState("");
  const searchTimer = useRef<ReturnType<typeof setTimeout>>(undefined);
  const [folderId, setFolderId] = useState(0);
  const [tagId, setTagId] = useState(0);
  const [workspaceId, setWorkspaceId] = useState(0);
  const [sort, setSort] = useState("created_desc");
  const [selectedIds, setSelectedIds] = useState<Set<number>>(new Set());
  const [batchTagId, setBatchTagId] = useState<number>(0);

  // Debounce the search input by 300ms
  useEffect(() => {
    clearTimeout(searchTimer.current);
    searchTimer.current = setTimeout(() => {
      setSearch(searchInput);
      setPage(1);
    }, 300);
    return () => clearTimeout(searchTimer.current);
  }, [searchInput]);

  const { data, error, isLoading, mutate } = useSWR(
    token ? [`links`, page, search, folderId, tagId, workspaceId, sort] : null,
    () => linksAPI.list(token!, page, PAGE_SIZE, search, folderId, tagId, workspaceId, sort)
  );

  const { data: folderData } = useSWR(
    token ? "folders" : null,
    () => foldersAPI.list(token!)
  );

  const { data: tagData } = useSWR(
    token ? "tags" : null,
    () => tagsAPI.list(token!)
  );

  const { data: workspaceData } = useSWR(
    token ? "workspaces" : null,
    () => workspacesAPI.list(token!)
  );

  const links: LinkItem[] = data?.links ?? [];
  const totalCount: number = data?.total_count ?? 0;
  const totalPages = Math.ceil(totalCount / PAGE_SIZE);
  const folders = folderData?.folders ?? [];
  const tags = tagData?.tags ?? [];
  const workspaces = workspaceData?.workspaces ?? [];

  const handleDelete = async (id: number) => {
    if (!token) return;
    try {
      await linksAPI.delete(token, id);
      mutate();
    } catch {
      toast.error(t("删除失败"));
      throw new Error("Delete failed");
    }
  };

  const handleSelect = (id: number, checked: boolean) => {
    setSelectedIds(prev => {
      const next = new Set(prev);
      if (checked) {
        next.add(id);
      } else {
        next.delete(id);
      }
      return next;
    });
  };

  const handleSelectAll = () => {
    if (selectedIds.size === links.length) {
      setSelectedIds(new Set());
    } else {
      setSelectedIds(new Set(links.map(l => l.id)));
    }
  };

  const handleBatchDelete = async () => {
    if (!token || selectedIds.size === 0) return;
    try {
      await linksAPI.batchDelete(token, Array.from(selectedIds));
      toast.success(
        t("已删除 ${selectedIds.size} 条链接").replace("${selectedIds.size}", String(selectedIds.size))
      );
      setSelectedIds(new Set());
      mutate();
    } catch {
      toast.error(t("批量删除失败"));
    }
  };

  const handleExport = async () => {
    if (!token) return;
    try {
      // Goes through the API client so the base URL is resolved in exactly one place and a non-JSON or
      // unreachable response is reported with its URL and status instead of a bare "failed".
      const csv = await linksAPI.export(token);
      const blob = new Blob([csv], { type: "text/csv;charset=utf-8" });
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url; a.download = "kada-links.csv"; a.click();
      URL.revokeObjectURL(url);
      toast.success(t("导出成功"));
    } catch (e: unknown) {
      toast.error(e instanceof Error ? e.message : t("导出失败"));
    }
  };

  const handleBatchTag = async () => {
    if (!token || selectedIds.size === 0 || batchTagId === 0) return;
    try {
      await linksAPI.batchTag(token, Array.from(selectedIds), batchTagId);
      toast.success(
        t("已为 ${selectedIds.size} 条链接添加标签").replace("${selectedIds.size}", String(selectedIds.size))
      );
      setBatchTagId(0);
      mutate();
    } catch {
      toast.error(t("批量打标签失败"));
    }
  };

  return (
    <div className="space-y-5">
      <LinksToolbar
        search={searchInput}
        onSearchChange={(v) => setSearchInput(v)}
        totalCount={totalCount}
        folders={folders}
        tags={tags}
        workspaces={workspaces}
        selectedFolderId={folderId}
        onFolderChange={(id) => { setFolderId(id); setPage(1); }}
        selectedTagId={tagId}
        onTagChange={(id) => { setTagId(id); setPage(1); }}
        selectedWorkspaceId={workspaceId}
        onWorkspaceChange={(id) => { setWorkspaceId(id); setPage(1); }}
        sort={sort}
        onSortChange={(s) => { setSort(s); setPage(1); }}
        onExport={handleExport}
      />

      {error ? (
        <div className="flex flex-col items-center justify-center py-20 text-center">
          <div className="flex size-16 items-center justify-center rounded-2xl bg-red-50 mb-4">
            <div className="text-3xl">😞</div>
          </div>
          <h3 className="text-lg font-semibold text-strong">{t("加载失败")}</h3>
          <p className="mt-1 text-sm text-muted">{t("请检查网络后重试")}</p>
          <button
            onClick={() => mutate()}
            className="mt-4 text-sm font-medium text-brand-ink hover:text-brand"
          >
            {t("重新加载")}
          </button>
        </div>
      ) : isLoading ? (
        <div className="space-y-2">
          {Array.from({ length: 8 }).map((_, i) => (
            <LinkCardPlaceholder key={i} />
          ))}
        </div>
      ) : links.length === 0 ? (
        <div className="flex flex-col items-center justify-center py-20 text-center">
          <div className="flex size-16 items-center justify-center rounded-2xl bg-muted-surface mb-4">
            <Link2 className="size-8 text-faint" />
          </div>
          <h3 className="text-lg font-semibold text-strong">
            {search ? t("没有匹配的链接") : t("创建你的第一个短链接")}
          </h3>
          <p className="mt-1 text-sm text-muted max-w-sm">
            {search
              ? t("换个关键词试试")
              : t("缩短、分享并追踪你的链接，兼容微信、QQ、小红书等平台")}
          </p>
          {!search && (
            <Link
              to="/dashboard/links/new"
              className="mt-5 inline-flex items-center gap-2 rounded-lg bg-indigo-600 px-5 py-2.5 text-sm font-medium text-white hover:bg-indigo-700 transition shadow-sm"
            >
              <Plus className="size-4" />
              {t("创建链接")}
            </Link>
          )}
        </div>
      ) : (
        <div className="space-y-2">
          {/* Select all bar */}
          <div className="flex items-center gap-2 px-1">
            <label className="flex items-center gap-2 text-sm text-muted cursor-pointer select-none">
              <input
                type="checkbox"
                checked={links.length > 0 && selectedIds.size === links.length}
                onChange={handleSelectAll}
                className="size-4 rounded border-line-strong text-brand-ink focus:ring-indigo-500 cursor-pointer"
              />
              {t("全选")}
            </label>
            {selectedIds.size > 0 && (
              <span className="text-xs text-faint">{t("已选 {n} 条", { n: selectedIds.size })}</span>
            )}
          </div>

          {links.map((link) => (
            <LinkCard
              key={link.id}
              link={link}
              onDelete={handleDelete}
              selectable
              selected={selectedIds.has(link.id)}
              onSelect={handleSelect}
            />
          ))}

          {/* Batch action bar */}
          {selectedIds.size > 0 && (
            <div className="sticky bottom-0 flex items-center justify-between gap-3 rounded-xl bg-canvas border border-line shadow-lg p-4 mt-4">
              <span className="text-sm font-medium text-body">{t("已选 {n} 条链接", { n: selectedIds.size })}</span>
              <div className="flex items-center gap-2">
                <select
                  value={batchTagId}
                  onChange={(e) => setBatchTagId(Number(e.target.value))}
                  className="rounded-lg border border-line px-3 py-1.5 text-sm text-muted focus:border-indigo-300 focus:outline-none bg-canvas"
                >
                  <option value={0}>{t("添加标签...")}</option>
                  {tags.map((tag: { id: number; name: string }) => (
                    <option key={tag.id} value={tag.id}>{tag.name}</option>
                  ))}
                </select>
                <button onClick={handleBatchTag} disabled={batchTagId === 0}
                  className="rounded-lg bg-indigo-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-indigo-700 disabled:opacity-40 disabled:cursor-not-allowed transition">
                  {t("批量打标签")}
                </button>
                <button onClick={handleBatchDelete}
                  className="rounded-lg bg-red-50 px-3 py-1.5 text-sm font-medium text-red-600 hover:bg-red-100 transition">
                  {t("批量删除")}
                </button>
                <button onClick={() => setSelectedIds(new Set())}
                  className="rounded-lg border border-line px-3 py-1.5 text-sm text-muted hover:bg-gray-50 transition">
                  {t("取消选择")}
                </button>
              </div>
            </div>
          )}

          {totalPages > 1 && (
            <div className="flex items-center justify-center gap-1 pt-6 pb-4">
              <button
                onClick={() => setPage((p) => Math.max(1, p - 1))}
                disabled={page === 1}
                className="px-3 py-1.5 text-sm rounded-lg border border-line bg-canvas hover:bg-gray-50 disabled:opacity-40 disabled:cursor-not-allowed transition"
              >
                {t("上一页")}
              </button>

              {generatePageNumbers(page, totalPages).map((p, i) =>
                p === null ? (
                  <span key={`dot-${i}`} className="px-1 text-faint text-sm">...</span>
                ) : (
                  <button
                    key={p}
                    onClick={() => setPage(p)}
                    className={`min-w-[2rem] h-8 text-sm rounded-lg transition ${
                      p === page
                        ? "bg-indigo-600 text-white font-medium shadow-sm"
                        : "border border-line bg-canvas hover:bg-gray-50 text-muted"
                    }`}
                  >
                    {p}
                  </button>
                )
              )}

              <button
                onClick={() => setPage((p) => p + 1)}
                disabled={page >= totalPages}
                className="px-3 py-1.5 text-sm rounded-lg border border-line bg-canvas hover:bg-gray-50 disabled:opacity-40 disabled:cursor-not-allowed transition"
              >
                {t("下一页")}
              </button>
            </div>
          )}
        </div>
      )}
    </div>
  );
}

function generatePageNumbers(current: number, total: number): (number | null)[] {
  if (total <= 7) return Array.from({ length: total }, (_, i) => i + 1);
  const pages: (number | null)[] = [1];
  if (current > 3) pages.push(null);
  for (let i = Math.max(2, current - 1); i <= Math.min(total - 1, current + 1); i++) {
    pages.push(i);
  }
  if (current < total - 2) pages.push(null);
  pages.push(total);
  return pages;
}
