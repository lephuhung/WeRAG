type P = { className?: string };
const base = (className?: string) => className ?? "h-[18px] w-[18px]";
const stroke = {
  fill: "none",
  stroke: "currentColor",
  strokeWidth: 1.6,
  strokeLinecap: "round" as const,
  strokeLinejoin: "round" as const,
};

export const IconSearch = ({ className }: P) => (
  <svg viewBox="0 0 24 24" className={base(className)} {...stroke}>
    <circle cx="11" cy="11" r="7" />
    <path d="m20 20-3.5-3.5" />
  </svg>
);

export const IconChat = ({ className }: P) => (
  <svg viewBox="0 0 24 24" className={base(className)} {...stroke}>
    <path d="M21 12a8 8 0 0 1-8 8H5l-2 2V12a8 8 0 0 1 8-8h2a8 8 0 0 1 8 8Z" />
  </svg>
);

export const IconBook = ({ className }: P) => (
  <svg viewBox="0 0 24 24" className={base(className)} {...stroke}>
    <path d="M4 19.5A2.5 2.5 0 0 1 6.5 17H20V4H6.5A2.5 2.5 0 0 0 4 6.5v13Z" />
    <path d="M4 19.5A2.5 2.5 0 0 0 6.5 22H20v-5" />
  </svg>
);

export const IconAgent = ({ className }: P) => (
  <svg viewBox="0 0 24 24" className={base(className)} {...stroke}>
    <rect x="5" y="7" width="14" height="12" rx="3" />
    <path d="M12 7V4M9 12h.01M15 12h.01M9 16h6" />
  </svg>
);

export const IconArtifact = ({ className }: P) => (
  <svg viewBox="0 0 24 24" className={base(className)} {...stroke}>
    <path d="M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8l-5-5Z" />
    <path d="M14 3v5h5M9 13h6M9 17h4" />
  </svg>
);

export const IconOrg = ({ className }: P) => (
  <svg viewBox="0 0 24 24" className={base(className)} {...stroke}>
    <circle cx="9" cy="8" r="3.2" />
    <path d="M3.5 20a5.5 5.5 0 0 1 11 0" />
    <path d="M16 8.5a3 3 0 1 1 0 5.7M17.5 15.5a5.5 5.5 0 0 1 3 4.5" />
  </svg>
);

export const IconSettings = ({ className }: P) => (
  <svg viewBox="0 0 24 24" className={base(className)} {...stroke}>
    <circle cx="12" cy="12" r="3" />
    <path d="M19 12a7 7 0 0 0-.1-1.2l2-1.5-2-3.4-2.3 1a7 7 0 0 0-2-1.2L14.2 3h-4l-.4 2.7a7 7 0 0 0-2 1.2l-2.3-1-2 3.4 2 1.5A7 7 0 0 0 5 12c0 .4 0 .8.1 1.2l-2 1.5 2 3.4 2.3-1a7 7 0 0 0 2 1.2l.4 2.7h4l.4-2.7a7 7 0 0 0 2-1.2l2.3 1 2-3.4-2-1.5c.1-.4.1-.8.1-1.2Z" />
  </svg>
);

export const IconPlus = ({ className }: P) => (
  <svg viewBox="0 0 24 24" className={base(className)} {...stroke}>
    <path d="M12 5v14M5 12h14" />
  </svg>
);

export const IconSend = ({ className }: P) => (
  <svg viewBox="0 0 24 24" className={base(className)} {...stroke}>
    <path d="M22 2 11 13M22 2l-7 20-4-9-9-4 20-7Z" />
  </svg>
);

export const IconDoc = ({ className }: P) => (
  <svg viewBox="0 0 24 24" className={base(className)} {...stroke}>
    <path d="M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8l-5-5Z" />
    <path d="M14 3v5h5" />
  </svg>
);

export const IconLogout = ({ className }: P) => (
  <svg viewBox="0 0 24 24" className={base(className)} {...stroke}>
    <path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4M16 17l5-5-5-5M21 12H9" />
  </svg>
);

export const IconPulse = ({ className }: P) => (
  <svg viewBox="0 0 24 24" className={base(className)} {...stroke}>
    <path d="M3 12h4l2-7 4 14 2-7h6" />
  </svg>
);

export const IconRefresh = ({ className }: P) => (
  <svg viewBox="0 0 24 24" className={base(className)} {...stroke}>
    <path d="M21 12a9 9 0 1 1-2.6-6.4M21 3v6h-6" />
  </svg>
);

export const IconChevronDown = ({ className }: P) => (
  <svg viewBox="0 0 24 24" className={base(className)} {...stroke}>
    <path d="m6 9 6 6 6-6" />
  </svg>
);

export const IconClose = ({ className }: P) => (
  <svg viewBox="0 0 24 24" className={base(className)} {...stroke}>
    <path d="M18 6 6 18M6 6l12 12" />
  </svg>
);

export const IconExternal = ({ className }: P) => (
  <svg viewBox="0 0 24 24" className={base(className)} {...stroke}>
    <path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6" />
    <polyline points="15 3 21 3 21 9" />
    <line x1="10" y1="14" x2="21" y2="3" />
  </svg>
);

export const IconDownload = ({ className }: P) => (
  <svg viewBox="0 0 24 24" className={base(className)} {...stroke}>
    <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4" />
    <polyline points="7 10 12 15 17 10" />
    <line x1="12" y1="15" x2="12" y2="3" />
  </svg>
);

export const IconTrash = ({ className }: P) => (
  <svg viewBox="0 0 24 24" className={base(className)} {...stroke}>
    <polyline points="3 6 5 6 21 6" />
    <path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2" />
  </svg>
);



