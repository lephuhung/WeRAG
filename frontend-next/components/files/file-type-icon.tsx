import { renderFileIconSvg } from "@/components/files/file-icon";

/* The file-type icon (DOCX blue, PDF red, XLSX green…) used wherever a file
 * is listed. A name without an extension takes fileType. Size it with
 * className (the icon keeps its 32×38 proportions). */
export function FileTypeIcon({ name, fileType, className = "h-7 w-6" }: { name: string; fileType?: string; className?: string }) {
  const ext = (fileType || "").toLowerCase().replace(/^\./, "");
  const iconName = /\.[a-z0-9]{1,5}$/i.test(name) || !ext ? name : `${name}.${ext}`;
  return (
    <span
      aria-hidden="true"
      className={`flex shrink-0 items-center justify-center ${className}`}
      dangerouslySetInnerHTML={{ __html: renderFileIconSvg(iconName) }}
    />
  );
}
