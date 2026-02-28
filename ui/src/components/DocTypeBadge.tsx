/**
 * Color-coded badge for doc_type. Shared by Search and Ask pages.
 */

const DOC_TYPE_COLORS: Record<string, string> = {
  text: "bg-blue-100 text-blue-800 dark:bg-blue-900/30 dark:text-blue-300",
  code: "bg-emerald-100 text-emerald-800 dark:bg-emerald-900/30 dark:text-emerald-300",
  pdf: "bg-rose-100 text-rose-800 dark:bg-rose-900/30 dark:text-rose-300",
  audio: "bg-violet-100 text-violet-800 dark:bg-violet-900/30 dark:text-violet-300",
  image: "bg-amber-100 text-amber-800 dark:bg-amber-900/30 dark:text-amber-300",
};

export function DocTypeBadge({ docType }: { docType?: string }) {
  const type = docType ?? "—";
  const c =
    DOC_TYPE_COLORS[type] ??
    "bg-zinc-200 text-zinc-700 dark:bg-zinc-700 dark:text-zinc-300";
  return (
    <span
      className={`inline-block rounded px-2 py-0.5 text-xs font-medium ${c}`}
      aria-label={type === "—" ? "Unknown document type" : `Document type: ${type}`}
    >
      {type}
    </span>
  );
}
