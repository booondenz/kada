import { Copy, ExternalLink, Pencil, Trash2, BarChart3, Check, QrCode, Download, X } from "lucide-react";
import { Link } from "react-router";
import { useState, useEffect } from "react";
import { toast } from "sonner";
import { useT } from "@/lib/i18n";

export interface LinkItem {
  id: number;
  short_code: string;
  short_url: string;
  original_url: string;
  title: string;
  description?: string;
  click_count: number;
  is_active: boolean;
  created_at: string;
  folder_id?: number;
  folder_name?: string;
  tags?: { id: number; name: string; color: string }[];
}

interface LinkCardProps {
  link: LinkItem;
  onDelete: (id: number) => void;
  workspaceSlug?: string;
  selectable?: boolean;
  selected?: boolean;
  onSelect?: (id: number, checked: boolean) => void;
}

export function LinkCard({ link, onDelete, selectable, selected, onSelect }: LinkCardProps) {
  const t = useT();
  const [copied, setCopied] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [showQR, setShowQR] = useState(false);
  const [qrDataURL, setQrDataURL] = useState<string | null>(null);

  const handleCopy = (e: React.MouseEvent) => {
    e.preventDefault();
    e.stopPropagation();
    navigator.clipboard.writeText(link.short_url);
    setCopied(true);
    toast.success(t("已复制到剪贴板"));
    setTimeout(() => setCopied(false), 2000);
  };

  const handleDelete = async (e: React.MouseEvent) => {
    e.preventDefault();
    e.stopPropagation();
    if (deleting) return;
    setDeleting(true);
    try {
      await onDelete(link.id);
      toast.success(t("链接已删除"));
    } catch {
      toast.error(t("删除失败"));
      setDeleting(false);
    }
  };

  const handleShowQR = async (e: React.MouseEvent) => {
    e.preventDefault();
    e.stopPropagation();
    setShowQR(true);
  };

  // Generate QR code when modal opens
  useEffect(() => {
    if (showQR && !qrDataURL) {
      import("qrcode").then((QRCode) => {
        const canvas = document.createElement("canvas");
        QRCode.toCanvas(canvas, link.short_url, {
          width: 256,
          margin: 2,
          color: { dark: "#000000", light: "#ffffff" },
          errorCorrectionLevel: "M",
        });
        setQrDataURL(canvas.toDataURL("image/png"));
      });
    }
  }, [showQR, qrDataURL, link.short_url]);

  return (
    <>
      <div className={`group relative bg-canvas rounded-xl border transition-all duration-200 ${
        selected ? "border-indigo-300 ring-2 ring-indigo-500/10 shadow-sm" : "border-line hover:border-line hover:shadow-md"
      }`}>
        <div className="flex items-start p-4 sm:p-5">
          {selectable && (
            <div className="pr-3 pt-0.5 shrink-0">
              <input
                type="checkbox"
                checked={selected}
                onChange={(e) => onSelect?.(link.id, e.target.checked)}
                onClick={(e) => e.stopPropagation()}
                className="size-4 rounded border-line-strong text-brand-ink focus:ring-indigo-500 cursor-pointer"
              />
            </div>
          )}
          <Link
            to={`/dashboard/links/${link.id}`}
            className="flex-1 min-w-0"
          >
          <div className="flex items-start justify-between gap-4">
            {/* Left: Info */}
            <div className="flex-1 min-w-0">
              <div className="flex items-center gap-2 mb-1">
                <h3 className="font-medium text-strong truncate text-sm sm:text-base">
                  {link.title || t("未命名链接")}
                </h3>
                <span
                  className={`shrink-0 px-1.5 py-0.5 text-[10px] font-medium rounded-full ${
                    link.is_active
                      ? "bg-green-100 text-green-700"
                      : "bg-muted-surface text-muted"
                  }`}
                >
                  {link.is_active ? t("启用") : t("停用")}
                </span>
              </div>

              <div className="flex items-center gap-2 text-sm">
                <span className="text-brand-ink font-mono text-xs sm:text-sm truncate">
                  {link.short_url}
                </span>
                <button
                  onClick={handleCopy}
                  className={`shrink-0 p-1 rounded transition-colors ${
                    copied
                      ? "text-green-500 bg-green-50"
                      : "text-faint hover:text-brand-ink"
                  }`}
                  title={t("复制链接")}
                >
                  {copied ? <Check className="w-3.5 h-3.5" /> : <Copy className="w-3.5 h-3.5" />}
                </button>
              </div>

              <p className="text-xs text-faint mt-1 truncate max-w-md">
                {link.original_url}
              </p>

              {link.description && (
                <p className="text-xs text-muted mt-1 line-clamp-1">{link.description}</p>
              )}
            </div>

            {/* Right: Stats & Actions */}
            <div className="flex items-center gap-3 shrink-0">
              <div className="hidden sm:flex items-center gap-1 text-sm">
                <BarChart3 className="w-3.5 h-3.5 text-faint" />
                <span className="font-semibold text-body tabular-nums">
                  {link.click_count.toLocaleString()}
                </span>
              </div>

              <button
                onClick={handleShowQR}
                className="p-1.5 text-faint hover:text-brand-ink hover:bg-brand-soft rounded-lg transition-colors"
                title={t("二维码")}
              >
                <QrCode className="w-4 h-4" />
              </button>

              <a
                href={link.short_url}
                target="_blank"
                rel="noopener noreferrer"
                onClick={(e) => e.stopPropagation()}
                className="p-1.5 text-faint hover:text-brand-ink hover:bg-brand-soft rounded-lg transition-colors"
                title={t("打开链接")}
              >
                <ExternalLink className="w-4 h-4" />
              </a>

              <Link
                to={`/dashboard/links/${link.id}`}
                onClick={(e) => e.stopPropagation()}
                className="p-1.5 text-faint hover:text-brand-ink hover:bg-brand-soft rounded-lg transition-colors"
                title={t("编辑")}
              >
                <Pencil className="w-4 h-4" />
              </Link>

              <button
                onClick={handleDelete}
                disabled={deleting}
                className="p-1.5 text-faint hover:text-red-500 hover:bg-red-50 rounded-lg transition-colors disabled:opacity-50"
                title={t("删除")}
              >
                <Trash2 className="w-4 h-4" />
              </button>
            </div>
          </div>
          </Link>
        </div>
      </div>

      {/* QR Code Modal */}
      {showQR && (
        <div
          className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 backdrop-blur-sm"
          onClick={() => setShowQR(false)}
        >
          <div
            className="bg-canvas rounded-2xl shadow-xl p-6 max-w-sm w-full mx-4"
            onClick={(e) => e.stopPropagation()}
          >
            <div className="flex items-center justify-between mb-4">
              <h3 className="font-semibold text-strong">{t("二维码")}</h3>
              <button
                onClick={() => setShowQR(false)}
                className="p-1.5 rounded-lg text-faint hover:text-muted hover:bg-muted-surface transition"
              >
                <X className="size-4" />
              </button>
            </div>

            <div className="flex flex-col items-center gap-4">
              {/* QR Image */}
              <div className="bg-canvas border border-line rounded-xl p-3">
                {qrDataURL ? (
                  // A data: URL produced locally by qrcode's canvas, so there is no remote host to
                  // allowlist and nothing for the image optimizer to fetch: an <img> passes it through.
                  <img src={qrDataURL} alt="QR Code" width={224} height={224} />
                ) : (
                  <div className="size-56 flex items-center justify-center">
                    <div className="size-8 animate-spin rounded-full border-2 border-indigo-600 border-t-transparent" />
                  </div>
                )}
              </div>

              <p className="text-sm font-mono text-muted text-center break-all">
                {link.short_url}
              </p>

              <div className="flex items-center gap-2 w-full">
                <button
                  onClick={() => {
                    navigator.clipboard.writeText(link.short_url);
                    toast.success(t("已复制"));
                  }}
                  className="flex-1 inline-flex items-center justify-center gap-1.5 rounded-lg border border-line px-4 py-2 text-sm font-medium text-body hover:bg-gray-50 transition"
                >
                  <Copy className="size-3.5" />
                  {t("复制链接")}
                </button>
                {qrDataURL && (
                  <a
                    href={qrDataURL}
                    download={`kada-qr-${link.short_code}.png`}
                    className="inline-flex items-center justify-center gap-1.5 rounded-lg bg-indigo-600 px-4 py-2 text-sm font-medium text-white hover:bg-indigo-700 transition"
                  >
                    <Download className="size-3.5" />
                    {t("下载")}
                  </a>
                )}
              </div>
            </div>
          </div>
        </div>
      )}
    </>
  );
}

/** Skeleton placeholder shown while loading */
export function LinkCardPlaceholder() {
  return (
    <div className="bg-canvas rounded-xl border border-line p-4 sm:p-5">
      <div className="flex items-start justify-between gap-4 animate-pulse">
        <div className="flex-1 space-y-2">
          <div className="flex items-center gap-2">
            <div className="h-5 w-32 bg-raised rounded-md sm:w-44" />
            <div className="h-4 w-10 bg-raised rounded-full" />
          </div>
          <div className="flex items-center gap-2">
            <div className="h-4 w-48 bg-muted-surface rounded-md" />
            <div className="h-3.5 w-3.5 bg-muted-surface rounded" />
          </div>
          <div className="h-3 w-64 bg-muted-surface rounded" />
        </div>
        <div className="flex items-center gap-3">
          <div className="hidden sm:block h-6 w-16 bg-raised rounded-md" />
          <div className="h-8 w-8 bg-muted-surface rounded-lg" />
          <div className="h-8 w-8 bg-muted-surface rounded-lg" />
          <div className="h-8 w-8 bg-muted-surface rounded-lg" />
        </div>
      </div>
    </div>
  );
}
