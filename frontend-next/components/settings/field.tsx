/* Label helpers for the settings forms.
 *
 * Every input must be named by its label: a <label htmlFor="…"> pointing
 * at the control's id, plus a consistent marker for required fields
 * (a "*" in the label and aria-required on the control).
 */
"use client";

import type { ReactNode } from "react";

export function RequiredMark() {
  return (
    <span aria-hidden="true" className="ml-0.5 font-semibold text-error">
      *
    </span>
  );
}

export function FieldLabel({
  htmlFor,
  required = false,
  children,
  className = "",
}: {
  htmlFor?: string;
  required?: boolean;
  children: ReactNode;
  className?: string;
}) {
  return (
    <label htmlFor={htmlFor} className={`caption mb-1.5 block text-muted ${className}`}>
      {children}
      {required ? <RequiredMark /> : null}
    </label>
  );
}

export default FieldLabel;
