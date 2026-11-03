import type { ReactNode } from "react";

/**
 * Renders a translated string that carries **bold** markers — the
 * translations keep the emphasis of the original markup without shipping
 * HTML through the dictionary.
 */
export function richText(text: string): ReactNode[] {
  return text.split(/(\*\*[^*]+\*\*)/g).map((part, index) =>
    part.startsWith("**") && part.endsWith("**") ? (
      <b key={index}>{part.slice(2, -2)}</b>
    ) : (
      <span key={index}>{part}</span>
    ),
  );
}
