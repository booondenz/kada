import { Search, Plus, Folder, Tag, Download, Building2 } from "lucide-react";
import { Link } from "react-router";
import { useT } from "@/lib/i18n";

export interface FilterOption {
  id: number;
  name: string;
  color?: string;
}

interface LinksToolbarProps {
  search: string;
  onSearchChange: (value: string) => void;
  totalCount: number;
  folders: FilterOption[];
  tags: FilterOption[];
  workspaces?: FilterOption[];
  selectedFolderId: number;
  onFolderChange: (id: number) => void;
  selectedTagId: number;
  onTagChange: (id: number) => void;
  selectedWorkspaceId?: number;
  onWorkspaceChange?: (id: number) => void;
  sort: string;
  onSortChange: (sort: string) => void;
  onExport?: () => void;
}

export function LinksToolbar({
  search,
  onSearchChange,
  totalCount,
  folders,
  tags,
  workspaces = [],
  selectedFolderId,
  onFolderChange,
  selectedTagId,
  onTagChange,
  selectedWorkspaceId = 0,
  onWorkspaceChange,
  sort,
  onSortChange,
  onExport,
}: LinksToolbarProps) {
  const t = useT();
  return (
    <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
      <div className="flex items-center gap-2 flex-1">
        {/* Search */}
        <div className="relative flex-1 max-w-xs">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 size-4 text-faint" />
          <input
            type="text"
            value={search}
            onChange={(e) => onSearchChange(e.target.value)}
            placeholder={t("搜索链接...")}
            className="w-full rounded-lg border border-line bg-canvas py-2 pl-9 pr-4 text-sm placeholder:text-faint focus:border-indigo-300 focus:outline-none focus:ring-2 focus:ring-indigo-100 transition"
          />
        </div>

        {/* Folder filter */}
        <div className="relative">
          <select
            value={selectedFolderId}
            onChange={(e) => onFolderChange(Number(e.target.value))}
            className="appearance-none rounded-lg border border-line bg-canvas py-2 pl-8 pr-8 text-sm text-muted focus:border-indigo-300 focus:outline-none focus:ring-2 focus:ring-indigo-100 transition cursor-pointer"
          >
            <option value={0}>{t("全部文件夹")}</option>
            {folders.map((f) => (
              <option key={f.id} value={f.id}>{f.name}</option>
            ))}
          </select>
          <Folder className="absolute left-2.5 top-1/2 -translate-y-1/2 size-3.5 text-faint pointer-events-none" />
        </div>

        {/* Tag filter */}
        <div className="relative">
          <select
            value={selectedTagId}
            onChange={(e) => onTagChange(Number(e.target.value))}
            className="appearance-none rounded-lg border border-line bg-canvas py-2 pl-8 pr-8 text-sm text-muted focus:border-indigo-300 focus:outline-none focus:ring-2 focus:ring-indigo-100 transition cursor-pointer"
          >
            <option value={0}>{t("全部标签")}</option>
            {tags.map((tag) => (
              <option key={tag.id} value={tag.id}>{tag.name}</option>
            ))}
          </select>
            <Tag className="absolute left-2.5 top-1/2 -translate-y-1/2 size-3.5 text-faint pointer-events-none" />
        </div>

        {/* Workspace filter */}
        {workspaces.length > 0 && onWorkspaceChange && (
          <div className="relative">
            <select
              value={selectedWorkspaceId}
              onChange={(e) => onWorkspaceChange(Number(e.target.value))}
              className="appearance-none rounded-lg border border-line bg-canvas py-2 pl-8 pr-8 text-sm text-muted focus:border-indigo-300 focus:outline-none focus:ring-2 focus:ring-indigo-100 transition cursor-pointer"
            >
              <option value={0}>{t("全部工作区")}</option>
              {workspaces.map((w) => (
                <option key={w.id} value={w.id}>{w.name}</option>
              ))}
            </select>
            <Building2 className="absolute left-2.5 top-1/2 -translate-y-1/2 size-3.5 text-faint pointer-events-none" />
          </div>
        )}
      </div>

      <div className="flex items-center gap-2">
        <select
          value={sort}
          onChange={(e) => onSortChange(e.target.value)}
          className="rounded-lg border border-line bg-canvas py-2 pl-3 pr-7 text-sm text-muted focus:border-indigo-300 focus:outline-none focus:ring-2 focus:ring-indigo-100 transition cursor-pointer appearance-none"
        >
          <option value="created_desc">{t("最新创建")}</option>
          <option value="created_asc">{t("最早创建")}</option>
          <option value="clicks_desc">{t("点击最多")}</option>
          <option value="clicks_asc">{t("点击最少")}</option>
        </select>
        <span className="text-sm text-faint">{totalCount} {t("条链接")}</span>
        {onExport && (
          <button
            type="button"
            onClick={onExport}
            className="inline-flex items-center gap-1.5 rounded-lg border border-line bg-canvas px-3 py-2 text-sm font-medium text-muted hover:bg-gray-50 transition"
          >
            <Download className="size-4" />
          </button>
        )}
        <Link
          to="/dashboard/links/new"
          className="inline-flex items-center gap-1.5 rounded-lg bg-indigo-600 px-4 py-2 text-sm font-medium text-white hover:bg-indigo-700 transition shadow-sm"
        >
          <Plus className="size-4" />
          {t("创建")}
        </Link>
      </div>
    </div>
  );
}
