"use client";

import type { ReactNode } from "react";
import styles from "./auth-shell.module.css";

/**
 * The card every authentication screen puts its form in: the sign-in
 * form, the sign-up form and the Word add-in approval. One surface, one
 * heading style, one footnote — the pages only fill in the content.
 */
export function AuthCard({
  kicker,
  title,
  subtitle,
  note,
  children,
  className = "",
}: {
  kicker?: string;
  title?: ReactNode;
  subtitle?: ReactNode;
  note?: ReactNode;
  children: ReactNode;
  className?: string;
}) {
  return (
    <div className={`${styles.authCard} ${className}`}>
      {kicker ? <p className={styles.kicker}>{kicker}</p> : null}
      {title ? <h2 className={styles.title}>{title}</h2> : null}
      {subtitle ? <p className={styles.subtitle}>{subtitle}</p> : null}
      {children}
      {note ? <p className={styles.note}>{note}</p> : null}
    </div>
  );
}
