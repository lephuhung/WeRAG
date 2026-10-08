import { renderFileIconSvg } from "@/components/files/file-icon";

/* An open document named inline in a chat message: its file-type icon, then
 * its name in blue, slightly bold. The "@" that marks it in the text is not
 * shown. */

export const DOC_MENTION_COLOR = "text-[#1d5bd8] dark:text-[#8ab4ff]";

export function DocMentionIcon({ name, className = "" }: { name: string; className?: string }) {
  return (
    <span
      aria-hidden="true"
      className={`inline-block h-[1.05em] w-[0.9em] shrink-0 align-[-0.15em] ${className}`}
      dangerouslySetInnerHTML={{ __html: renderFileIconSvg(name) }}
    />
  );
}

/** "@name" token of a sent message. */
export function DocMention({ token }: { token: string }) {
  const name = token.replace(/^@/, "");
  return (
    <span title={name} className={`inline font-semibold ${DOC_MENTION_COLOR}`}>
      <DocMentionIcon name={name} className="mr-[0.2em]" />
      {name}
    </span>
  );
}
