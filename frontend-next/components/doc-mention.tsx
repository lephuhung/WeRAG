import { renderFileIconSvg } from "@/components/files/file-icon";

/* An open document named inline in a chat message: its file-type icon, then
 * its name in blue, slightly bold. The "@" that marks it in the text is not
 * shown. */

/* The inline document name used a hardcoded blue pair that never followed
 * the theme; --color-accent carries the same meaning in light and dark. */
export const DOC_MENTION_COLOR = "text-accent";

export function DocMentionIcon({ name, className = "" }: { name: string; className?: string }) {
  return (
    <span
      aria-hidden="true"
      className={`inline-block h-[1.05em] w-[0.9em] shrink-0 align-[-0.15em] ${className}`}
      dangerouslySetInnerHTML={{ __html: renderFileIconSvg(name) }}
    />
  );
}

/** "@name" token of a sent message.
 *
 * `className` REPLACES the default colour instead of adding to it: inside
 * the user bubble the background is a solid accent, so the mention needs
 * the bubble's own ink — two colour utilities on one element would race
 * on Tailwind's output order. */
export function DocMention({ token, className }: { token: string; className?: string }) {
  const name = token.replace(/^@/, "");
  return (
    <span title={name} className={`inline font-semibold ${className ?? DOC_MENTION_COLOR}`}>
      <DocMentionIcon name={name} className="mr-[0.2em]" />
      {name}
    </span>
  );
}
