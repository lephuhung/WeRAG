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

export const IconEdit = ({ className }: P) => (
  <svg viewBox="0 0 24 24" className={base(className)} {...stroke}>
    <path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7" />
    <path d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z" />
  </svg>
);

export const IconEye = ({ className }: P) => (
  <svg viewBox="0 0 24 24" className={base(className)} {...stroke}>
    <path d="M2 12s3-7 10-7 10 7 10 7-3 7-10 7-10-7-10-7Z" />
    <circle cx="12" cy="12" r="3" />
  </svg>
);

export const IconCopy = ({ className }: P) => (
  <svg viewBox="0 0 24 24" className={base(className)} {...stroke}>
    <rect width="13" height="13" x="9" y="9" rx="2" ry="2" />
    <path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1" />
  </svg>
);

export const IconCode = ({ className }: P) => (
  <svg viewBox="0 0 24 24" className={base(className)} {...stroke}>
    <polyline points="16 18 22 12 16 6" />
    <polyline points="8 6 2 12 8 18" />
  </svg>
);

export const IconPower = ({ className }: P) => (
  <svg viewBox="0 0 24 24" className={base(className)} {...stroke}>
    <path d="M12 2v10" />
    <path d="M18.4 6.6a9 9 0 1 1-12.77.01" />
  </svg>
);

export const IconDocReader = ({ className }: P) => (
  <svg viewBox="0 0 24 24" className={base(className)} {...stroke}>
    <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z" />
    <polyline points="14 2 14 8 20 8" />
    <line x1="16" y1="13" x2="8" y2="13" />
    <line x1="16" y1="17" x2="8" y2="17" />
    <polyline points="10 9 9 9 8 9" />
  </svg>
);

export const IconParserEngine = ({ className }: P) => (
  <svg viewBox="0 0 24 24" className={base(className)} {...stroke}>
    <path d="M4 22h14a2 2 0 0 0 2-2V7.5L14.5 2H6a2 2 0 0 0-2 2v4" />
    <polyline points="14 2 14 8 20 8" />
    <path d="m3 15 2 2 4-4" />
  </svg>
);

export const IconStorageEngine = ({ className }: P) => (
  <svg viewBox="0 0 24 24" className={base(className)} {...stroke}>
    <ellipse cx="12" cy="5" rx="9" ry="3" />
    <path d="M21 12c0 1.66-4 3-9 3s-9-1.34-9-3" />
    <path d="M3 5v14c0 1.66 4 3 9 3s9-1.34 9-3V5" />
  </svg>
);

export const IconCheck = ({ className }: P) => (
  <svg viewBox="0 0 24 24" className={base(className)} {...stroke}>
    <polyline points="20 6 9 17 4 12" />
  </svg>
);

export const IconGraph = ({ className }: P) => (
  <svg viewBox="0 0 24 24" className={base(className)} {...stroke}>
    <circle cx="18" cy="5" r="3" />
    <circle cx="6" cy="12" r="3" />
    <circle cx="18" cy="19" r="3" />
    <line x1="8.59" y1="13.51" x2="15.42" y2="17.49" />
    <line x1="15.41" y1="6.51" x2="8.59" y2="10.49" />
  </svg>
);

export const IconFork = ({ className }: P) => (
  <svg viewBox="0 0 24 24" className={base(className)} {...stroke}>
    <line x1="6" y1="3" x2="6" y2="15" />
    <circle cx="18" cy="6" r="3" />
    <circle cx="6" cy="18" r="3" />
    <path d="M18 9a9 9 0 0 1-9 9" />
  </svg>
);

export const IconBookmark = ({ className }: P) => (
  <svg viewBox="0 0 24 24" className={base(className)} {...stroke}>
    <path d="m19 21-7-4-7 4V5a2 2 0 0 1 2-2h10a2 2 0 0 1 2 2v16z" />
  </svg>
);

export const IconChevronUp = ({ className }: P) => (
  <svg viewBox="0 0 24 24" className={base(className)} {...stroke}>
    <path d="m18 15-6-6-6 6" />
  </svg>
);

export const IconChevronRight = ({ className }: P) => (
  <svg viewBox="0 0 24 24" className={base(className)} {...stroke}>
    <path d="m9 18 6-6-6-6" />
  </svg>
);

export const IconArrowUp = ({ className }: P) => (
  <svg viewBox="0 0 24 24" className={base(className)} {...stroke}>
    <path d="M12 19V5m-7 7 7-7 7 7" />
  </svg>
);

export const IconFileSearch = ({ className }: P) => (
  <svg viewBox="0 0 24 24" className={base(className)} {...stroke}>
    <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z" />
    <path d="M14 2v6h6" />
    <circle cx="11.5" cy="14.5" r="2.5" />
    <path d="m13.3 16.3 2 2" />
  </svg>
);

export const IconClock = ({ className }: P) => (
  <svg viewBox="0 0 24 24" className={base(className)} {...stroke}>
    <circle cx="12" cy="12" r="9" />
    <path d="M12 7v5l3.5 2" />
  </svg>
);

export const IconMinusCircle = ({ className }: P) => (
  <svg viewBox="0 0 24 24" className={base(className)} {...stroke}>
    <circle cx="12" cy="12" r="9" />
    <path d="M8 12h8" />
  </svg>
);

const fill = { fill: "currentColor", stroke: "none" };

export const IconCheckCircleFilled = ({ className }: P) => (
  <svg viewBox="0 0 24 24" className={base(className)} {...fill}>
    <path d="M12 2a10 10 0 1 0 0 20 10 10 0 0 0 0-20zm-1.2 14.4-4-4 1.4-1.4 2.6 2.6 5.6-5.6 1.4 1.4z" />
  </svg>
);

export const IconErrorCircleFilled = ({ className }: P) => (
  <svg viewBox="0 0 24 24" className={base(className)} {...fill}>
    <path d="M12 2a10 10 0 1 0 0 20 10 10 0 0 0 0-20zm-1 5h2v7h-2zm0 9h2v2h-2z" />
  </svg>
);

export const IconInfoCircleFilled = ({ className }: P) => (
  <svg viewBox="0 0 24 24" className={base(className)} {...fill}>
    <path d="M12 2a10 10 0 1 0 0 20 10 10 0 0 0 0-20zm-1 6h2v2h-2zm0 3h2v7h-2z" />
  </svg>
);

export const IconInfoCircle = ({ className }: P) => (
  <svg viewBox="0 0 24 24" className={base(className)} {...stroke}>
    <circle cx="12" cy="12" r="9" />
    <path d="M12 8h.01M12 11v5" />
  </svg>
);

export const IconMoreHorizontal = ({ className }: P) => (
  <svg viewBox="0 0 24 24" className={base(className)} {...fill}>
    <circle cx="5" cy="12" r="1.8" />
    <circle cx="12" cy="12" r="1.8" />
    <circle cx="19" cy="12" r="1.8" />
  </svg>
);

export const IconFolder = ({ className }: P) => (
  <svg viewBox="0 0 24 24" className={base(className)} {...stroke}>
    <path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v9a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z" />
  </svg>
);

export const IconUser = ({ className }: P) => (
  <svg viewBox="0 0 24 24" className={base(className)} {...stroke}>
    <circle cx="12" cy="8" r="3.6" />
    <path d="M5 20a7 7 0 0 1 14 0" />
  </svg>
);

export const IconImage = ({ className }: P) => (
  <svg viewBox="0 0 24 24" className={base(className)} {...stroke}>
    <rect x="3" y="4" width="18" height="16" rx="2" />
    <circle cx="9" cy="10" r="1.6" />
    <path d="m4 18 5-5 3 3 4-4 4 4" />
  </svg>
);

export const IconIdCard = ({ className }: P) => (
  <svg viewBox="0 0 24 24" className={base(className)} {...stroke}>
    <rect x="3" y="5" width="18" height="14" rx="2" />
    <circle cx="8.5" cy="11" r="1.8" />
    <path d="M5.8 16.2a2.9 2.9 0 0 1 5.4 0M13.5 9.5H18M13.5 13H18M13.5 16.5h3" />
  </svg>
);

export const IconPhone = ({ className }: P) => (
  <svg viewBox="0 0 24 24" className={base(className)} {...stroke}>
    <path d="M5 4h3.5l1.5 4-2 1.5a11 11 0 0 0 5.5 5.5L15 13.5l4 1.5V18a2 2 0 0 1-2 2A15 15 0 0 1 4 7a3 3 0 0 1 1-3Z" />
  </svg>
);

export const IconMapPin = ({ className }: P) => (
  <svg viewBox="0 0 24 24" className={base(className)} {...stroke}>
    <path d="M12 21s-6.5-5.6-6.5-10.5A6.5 6.5 0 0 1 12 4a6.5 6.5 0 0 1 6.5 6.5C18.5 15.4 12 21 12 21Z" />
    <circle cx="12" cy="10.5" r="2" />
  </svg>
);

export const IconCalendar = ({ className }: P) => (
  <svg viewBox="0 0 24 24" className={base(className)} {...stroke}>
    <rect x="3.5" y="5" width="17" height="15.5" rx="2" />
    <path d="M3.5 9.5h17M8 3v4M16 3v4" />
  </svg>
);

export const IconBulb = ({ className }: P) => (
  <svg viewBox="0 0 24 24" className={base(className)} {...stroke}>
    <path d="M9 18h6M10 21h4" />
    <path d="M12 3a6 6 0 0 0-4 10.5c.8.7 1.3 1.4 1.5 2.5h5c.2-1.1.7-1.8 1.5-2.5A6 6 0 0 0 12 3Z" />
  </svg>
);

export const IconGlobe = ({ className }: P) => (
  <svg viewBox="0 0 24 24" className={base(className)} {...stroke}>
    <circle cx="12" cy="12" r="9" />
    <path d="M3 12h18M12 3c2.5 2.6 4 5.6 4 9s-1.5 6.4-4 9c-2.5-2.6-4-5.6-4-9s1.5-6.4 4-9Z" />
  </svg>
);








