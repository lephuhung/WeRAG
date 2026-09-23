"use client";

/* Port of AIRAG frontend PeopleCard.tsx: consolidated person cards for
 * people_lookup tool results. Records sharing `_person_group` (backend
 * cross-schema consolidation) merge into ONE card; fields render via
 * FIELD_CONFIG labels, unmapped fields still render with a prettified key so
 * no returned attribute is silently dropped. */

import { useMemo, useState, type ReactNode } from "react";
import {
  IconCalendar,
  IconCheck,
  IconCopy,
  IconDoc,
  IconExternal,
  IconFileSearch,
  IconIdCard,
  IconMapPin,
  IconOrg,
  IconPhone,
  IconPulse,
  IconUser,
} from "@/components/icons";
import { copyToClipboard } from "@/lib/clipboard";

export type PeopleRecord = Record<string, unknown> & {
  _source_schema?: string;
  _person_group?: number;
};

const ic = "h-3 w-3";

/** Field display config: schema-agnostic keys → label + icon */
const FIELD_CONFIG: Record<string, { label: string; icon: ReactNode }> = {
  hoTen: { label: "Họ tên", icon: <IconUser className={ic} /> },
  HO_TEN: { label: "Họ tên", icon: <IconUser className={ic} /> },
  TenHoiVien: { label: "Họ tên", icon: <IconUser className={ic} /> },
  ho_ten: { label: "Họ tên", icon: <IconUser className={ic} /> },
  fullName: { label: "Họ tên", icon: <IconUser className={ic} /> },
  tenKhachHang: { label: "Họ tên", icon: <IconUser className={ic} /> },
  maSoBhxh: { label: "Mã BHXH", icon: <IconIdCard className={ic} /> },
  soTheBhyt: { label: "Số thẻ BHYT", icon: <IconIdCard className={ic} /> },
  ngaySinhHienThi: { label: "Ngày sinh", icon: <IconCalendar className={ic} /> },
  NGAY_SINH: { label: "Ngày sinh", icon: <IconCalendar className={ic} /> },
  NgaySinh: { label: "Ngày sinh", icon: <IconCalendar className={ic} /> },
  namsinh: { label: "Năm sinh", icon: <IconCalendar className={ic} /> },
  fullNam: { label: "Năm sinh", icon: <IconCalendar className={ic} /> },
  ngayDangKy: { label: "Ngày đăng ký", icon: <IconCalendar className={ic} /> },
  tuNgay: { label: "Từ ngày", icon: <IconCalendar className={ic} /> },
  denNgay: { label: "Đến ngày", icon: <IconCalendar className={ic} /> },
  soCmnd: { label: "Số CMND/CCCD", icon: <IconFileSearch className={ic} /> },
  cmnd: { label: "Số CMND", icon: <IconFileSearch className={ic} /> },
  SoDinhDanh: { label: "Số định danh", icon: <IconFileSearch className={ic} /> },
  MA_DOI_TUONG: { label: "Mã định danh", icon: <IconFileSearch className={ic} /> },
  PID: { label: "Mã PID", icon: <IconFileSearch className={ic} /> },
  dienThoai: { label: "Điện thoại", icon: <IconPhone className={ic} /> },
  SoDienThoai: { label: "Điện thoại", icon: <IconPhone className={ic} /> },
  DIEN_THOAI_ME: { label: "Điện thoại mẹ", icon: <IconPhone className={ic} /> },
  mobile: { label: "Điện thoại", icon: <IconPhone className={ic} /> },
  so_dien_thoai: { label: "Điện thoại", icon: <IconPhone className={ic} /> },
  phone: { label: "Điện thoại", icon: <IconPhone className={ic} /> },
  diaChi: { label: "Địa chỉ", icon: <IconMapPin className={ic} /> },
  DiaChi: { label: "Địa chỉ", icon: <IconMapPin className={ic} /> },
  dia_chi: { label: "Địa chỉ", icon: <IconMapPin className={ic} /> },
  diaChiCapDien: { label: "Địa chỉ", icon: <IconMapPin className={ic} /> },
  coSoKCB: { label: "CS KCB", icon: <IconOrg className={ic} /> },
  trangThaiThe: { label: "Trạng thái thẻ", icon: <IconPulse className={ic} /> },
  tyLeBhyt: { label: "Tỷ lệ BHYT", icon: <IconPulse className={ic} /> },
  TenHangHoiVien: { label: "Hạng hội viên", icon: <IconDoc className={ic} /> },
  SoTheHoiVien: { label: "Số thẻ hội viên", icon: <IconDoc className={ic} /> },
  DiemHoiVien: { label: "Điểm hội viên", icon: <IconDoc className={ic} /> },
  TEN_ME: { label: "Tên mẹ", icon: <IconUser className={ic} /> },
  GIOI_TINH: { label: "Giới tính", icon: <IconUser className={ic} /> },
  gioi_tinh: { label: "Giới tính", icon: <IconUser className={ic} /> },
  uid: { label: "Facebook UID", icon: <IconExternal className={ic} /> },
  uids: { label: "Facebook UID", icon: <IconExternal className={ic} /> },
};

/** Fields never rendered (bookkeeping injected by the backend + Mongo noise). */
const SKIP_FIELDS = new Set([
  "_id",
  "id",
  "_source_schema",
  "_person_group",
  "lookup_type",
  "found",
  "persons",
  "display",
  "__v",
  "stt",
  "STT",
  "createdAt",
  "updatedAt",
  "created_at",
  "updated_at",
  "created_by",
  "updated_by",
  "synced_at",
  "imported_at",
  "row_number",
]);

const NAME_KEYS = ["hoTen", "HO_TEN", "TenHoiVien", "ho_ten", "fullName", "tenKhachHang"];
const PHONE_KEYS = [
  "soDienThoai",
  "SoDienThoai",
  "dienThoai",
  "mobile",
  "so_dien_thoai",
  "DIEN_THOAI_ME",
  "phone",
];

const SCHEMA_LABELS: Record<string, string> = {
  bhxh: "BHXH",
  lg: "LG Hội viên",
  vacxin: "Tiêm chủng",
  evn: "Điện lực",
  cv19: "Covid 19",
  uids: "UIDS",
  vnvc: "VNVC",
};

interface MergedPerson {
  groupKey: string;
  name: string;
  sources: string[];
  fields: [string, string[]][];
}

function isObjectIdLike(v: unknown): boolean {
  return typeof v === "string" && /^[0-9a-fA-F]{24}$/.test(v);
}

function formatValue(key: string, v: unknown): string {
  if (v === undefined || v === null) return "";
  if (key === "GIOI_TINH" || key === "gioi_tinh") {
    if (String(v) === "1") return "Nam";
    if (String(v) === "0") return "Nữ";
  }
  if (Array.isArray(v)) {
    return v
      .map((e) => formatValue(key, e))
      .filter(Boolean)
      .join(" · ");
  }
  if (typeof v === "object") {
    try {
      return JSON.stringify(v);
    } catch {
      return "";
    }
  }
  const s = String(v).trim();
  return s === "None" || s === "<nil>" ? "" : s;
}

/** Prettify an unmapped field key: camelCase / snake_case → spaced label. */
function fieldLabel(key: string): string {
  if (FIELD_CONFIG[key]) return FIELD_CONFIG[key].label;
  return key
    .replace(/[_-]+/g, " ")
    .replace(/([a-z0-9])([A-Z])/g, "$1 $2")
    .trim()
    .toLowerCase()
    .replace(/^\w/, (c) => c.toUpperCase());
}

function getNameField(record: Record<string, unknown>): string {
  for (const key of NAME_KEYS) {
    const v = record[key];
    if (typeof v === "string" && v.trim()) return v.trim();
  }
  return "";
}

function mergePeople(people: PeopleRecord[]): MergedPerson[] {
  const fieldOrder = Object.keys(FIELD_CONFIG);
  const groups = new Map<string, PeopleRecord[]>();
  const order: string[] = [];

  people.forEach((p, i) => {
    const g = p._person_group;
    const key = g !== undefined && g !== null ? `g${g}` : `i${i}`;
    if (!groups.has(key)) {
      groups.set(key, []);
      order.push(key);
    }
    groups.get(key)!.push(p);
  });

  return order.map((key) => {
    const recs = groups.get(key)!;
    const sources: string[] = [];
    const fieldMap = new Map<string, string[]>();
    let name = "";

    for (const r of recs) {
      const schema = (r._source_schema as string) || "";
      if (schema && !sources.includes(schema)) sources.push(schema);
      if (!name) name = getNameField(r);
      for (const [k, v] of Object.entries(r)) {
        if (SKIP_FIELDS.has(k) || k.startsWith("_") || v === undefined || v === null) continue;
        if (isObjectIdLike(v)) continue;
        const val = formatValue(k, v);
        if (!val) continue;
        const arr = fieldMap.get(k) ?? [];
        if (!arr.includes(val)) arr.push(val);
        fieldMap.set(k, arr);
      }
    }

    // Fallback header when no name (e.g. uid-only records).
    if (!name) {
      const phoneVal = PHONE_KEYS.map((k) => fieldMap.get(k)?.[0]).find(Boolean);
      const uidVal = fieldMap.get("uid")?.[0] || fieldMap.get("uids")?.[0];
      name = phoneVal || (uidVal ? `UID ${uidVal}` : "") || "(Không có tên)";
    }

    // Only labelled fields render — raw/unmapped columns are usually internal
    // Mongo noise (mirrors AIRAG: FIELD_CONFIG is the whitelist).
    const fields = Array.from(fieldMap.entries())
      .filter(([k]) => FIELD_CONFIG[k])
      .sort(([a], [b]) => fieldOrder.indexOf(a) - fieldOrder.indexOf(b));

    return { groupKey: key, name, sources, fields };
  });
}

export function PeopleCard({
  people,
  isLoadingMore,
}: {
  people: PeopleRecord[];
  isLoadingMore?: boolean;
}) {
  const [copiedKey, setCopiedKey] = useState<string | null>(null);
  const merged = useMemo(() => mergePeople(people), [people]);

  if (!people || people.length === 0) return null;

  const schemaLabel = (s: string) => SCHEMA_LABELS[s] || s;

  const handleCopyCard = (person: MergedPerson) => {
    const sources = person.sources.map(schemaLabel).join(", ") || "Unknown";
    const lines = [`Thông tin: ${person.name}`, `Nguồn dữ liệu: ${sources}`, ""];
    for (const [key, vals] of person.fields) {
      lines.push(`- ${fieldLabel(key)}: ${vals.join(" · ")}`);
    }
    void copyToClipboard(lines.join("\n"));
    setCopiedKey(person.groupKey);
    setTimeout(() => setCopiedKey(null), 2000);
  };

  return (
    <div className="my-3 space-y-2">
      {merged.map((person) => {
        const isMulti = person.sources.length > 1;
        return (
          <div
            key={person.groupKey}
            className="card relative w-fit min-w-[260px] max-w-full p-4 transition-shadow hover:shadow-md"
          >
            <button
              type="button"
              onClick={() => handleCopyCard(person)}
              className={`btn btn-ghost btn-sm absolute right-3 top-3 p-1.5 ${
                copiedKey === person.groupKey ? "text-success" : "text-muted hover:text-ink"
              }`}
              title="Copy"
            >
              {copiedKey === person.groupKey ? (
                <IconCheck className="h-4 w-4" />
              ) : (
                <IconCopy className="h-4 w-4" />
              )}
            </button>

            {/* Header: avatar initial + name + source badges */}
            <div className="mb-3.5 flex items-center gap-3 pr-10">
              <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-xl border border-primary/20 bg-primary/10 text-sm font-bold text-primary">
                {person.name.charAt(0).toUpperCase()}
              </div>
              <div className="min-w-0 flex-1">
                <p className="truncate text-[15px] font-bold tracking-tight text-ink">
                  {person.name}
                </p>
                {person.sources.length > 0 && (
                  <div className="mt-1 flex flex-wrap items-center gap-1.5">
                    {isMulti && (
                      <span className="badge-pill bg-emerald-500/10 text-[9px] font-bold text-emerald-600">
                        {person.sources.length} nguồn
                      </span>
                    )}
                    {person.sources.map((s) => (
                      <span
                        key={s}
                        className="badge-pill bg-primary/10 text-[9px] font-bold text-primary"
                      >
                        {schemaLabel(s)}
                      </span>
                    ))}
                  </div>
                )}
              </div>
            </div>

            {/* Fields grid (merged across sources) */}
            <div className="grid grid-cols-1 gap-x-6 gap-y-2.5 sm:grid-cols-2">
              {person.fields.map(([key, vals]) => (
                <div key={key} className="flex items-start gap-2.5">
                  <div className="mt-0.5 flex h-5 w-5 shrink-0 scale-90 items-center justify-center rounded-md bg-surface-strong text-muted">
                    {FIELD_CONFIG[key]?.icon ?? <IconDoc className={ic} />}
                  </div>
                  <div className="min-w-0">
                    <p className="mb-0.5 text-[10px] font-semibold uppercase leading-none tracking-tight text-muted-soft">
                      {fieldLabel(key)}
                    </p>
                    {key === "uid" || key === "uids" ? (
                      <div className="flex flex-col gap-0.5">
                        {vals.map((v) => (
                          <a
                            key={v}
                            href={`https://facebook.com/${v}`}
                            target="_blank"
                            rel="noopener noreferrer"
                            className="truncate text-[12px] font-medium text-primary hover:underline"
                          >
                            {v}
                          </a>
                        ))}
                      </div>
                    ) : (
                      <p className="break-words text-[12px] font-medium text-ink/90">
                        {vals.join(" · ")}
                      </p>
                    )}
                  </div>
                </div>
              ))}
            </div>
          </div>
        );
      })}

      {isLoadingMore && (
        <div className="flex items-center justify-center gap-2 rounded-lg border border-dashed border-hairline p-3 text-muted">
          <span className="inline-block h-3.5 w-3.5 animate-spin rounded-full border-2 border-muted-soft border-t-transparent" />
          <span className="text-xs font-medium">Đang tìm kiếm thêm cơ sở dữ liệu...</span>
        </div>
      )}
    </div>
  );
}
