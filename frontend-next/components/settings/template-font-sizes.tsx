/* Cỡ chữ và lề văn bản mẫu: font sizes (pt) and page margins (mm) of the
 * NĐ30 templates drafted from chat.
 * The SuperAdmin edits the platform default column, the workspace admin the
 * unit's override column ("follow" leaves a row to the platform); everyone
 * else reads the sizes in use. The server checks the NĐ30 ranges. */
"use client";

import { useCallback, useEffect, useState } from "react";
import {
  getTemplateStyle,
  updatePlatformTemplateStyle,
  updateTenantTemplateStyle,
  type TemplateFontSizes,
  type TemplateStyle,
} from "@/lib/api/template-style";
import { useT, type LocaleKey } from "@/lib/i18n";
import { DataTable } from "@/components/settings/data-table";
import { SectionHeader } from "@/components/settings/section-header";
import { Notice } from "@/components/settings/notice";
import { Loading } from "@/components/settings/loading";
import { useToast } from "@/components/toast";

/* What each component looks like in a document (document text, the same in
 * every UI language). */
const EXAMPLES: Record<string, string> = {
  co_quan_chu_quan: "BỘ CÔNG AN",
  co_quan_ban_hanh: "CÔNG AN TỈNH THỪA THIÊN HUẾ",
  quoc_hieu: "CỘNG HÒA XÃ HỘI CHỦ NGHĨA VIỆT NAM",
  tieu_ngu: "Độc lập - Tự do - Hạnh phúc",
  trich_yeu_cong_van: "V/v triển khai kế hoạch chuyển đổi số",
  dia_danh_ngay_thang: "Huế, ngày 17 tháng 10 năm 2026",
  ten_loai: "KẾ HOẠCH, QUYẾT ĐỊNH, BÁO CÁO",
  trich_yeu: "Triển khai chuyển đổi số năm 2026",
  tham_quyen_ban_hanh: "GIÁM ĐỐC CÔNG AN TỈNH",
  noi_dung: "Kính gửi: …; Căn cứ …; các đoạn nội dung",
  chuc_vu_nguoi_ky: "KT. GIÁM ĐỐC / PHÓ GIÁM ĐỐC",
  ho_ten_nguoi_ky: "Nguyễn Văn A",
  so_trang: "2, 3, … (giữa lề trên, từ trang 2)",
  le_trai: "Lề rộng hơn để đóng gáy",
};

export function TemplateFontSizesSettings({
  canEditTenant,
  isSystemAdmin,
}: {
  canEditTenant: boolean;
  isSystemAdmin: boolean;
}) {
  const { t } = useT();
  const toast = useToast();
  const [style, setStyle] = useState<TemplateStyle | null>(null);
  const [platform, setPlatform] = useState<TemplateFontSizes>({});
  const [tenant, setTenant] = useState<TemplateFontSizes>({});
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");

  const apply = (s: TemplateStyle) => {
    setStyle(s);
    setPlatform(
      Object.fromEntries(
        s.components.map((c) => [c.key, s.platform[c.key] ?? c.default]),
      ),
    );
    setTenant({ ...s.tenant });
  };

  const load = useCallback(async () => {
    try {
      apply((await getTemplateStyle()).data);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const run = async (save: () => Promise<unknown>) => {
    setSaving(true);
    setError("");
    try {
      await save();
      await load();
      toast.success(t("templateStyle.saved"));
    } catch (e) {
      setError(
        t("templateStyle.saveFailed", {
          error: e instanceof Error ? e.message : String(e),
        }),
      );
    } finally {
      setSaving(false);
    }
  };

  if (!style)
    return error ? (
      <Notice tone="error" className="mt-4">{error}</Notice>
    ) : (
      <Loading className="py-8" />
    );

  const range = (min: number, max: number) =>
    Array.from({ length: max - min + 1 }, (_, i) => min + i);

  return (
    <div className="flex flex-col gap-3 border-t border-hairline pt-4">
      <SectionHeader title={t("templateStyle.title")} subtitle={t("templateStyle.desc")} />
      <DataTable
        columns={[
          { key: "component", header: t("templateStyle.component") },
          { key: "range", header: t("templateStyle.range") },
          { key: "platform", header: t("templateStyle.platform") },
          { key: "tenant", header: t("templateStyle.tenant") },
          { key: "effective", header: t("templateStyle.effective") },
        ]}
        rows={style.components}
        rowKey={(c) => c.key}
        minWidth="min-w-[720px]"
        renderGroupRow={(c) =>
          c.unit !== style.components[style.components.indexOf(c) - 1]?.unit ? (
            <tr>
              <td colSpan={5} className="px-5 pb-1 pt-3 text-[13px] font-medium text-ink">
                {t(
                  c.unit === "mm"
                    ? "templateStyle.groupMargins"
                    : "templateStyle.groupFonts",
                )}
              </td>
            </tr>
          ) : null
        }
        renderRow={(c) => {
              const inherited = platform[c.key] ?? c.default;
              const inUse = tenant[c.key] ?? inherited;
              return (
                <>
                    <td className="px-5 py-3.5 align-middle">
                      {t(`templateStyle.c.${c.key}` as LocaleKey)}
                      {EXAMPLES[c.key] && (
                        <span className="caption block text-muted">
                          {t("templateStyle.example", {
                            text: EXAMPLES[c.key],
                          })}
                        </span>
                      )}
                    </td>
                    <td className="caption px-5 py-3.5 align-middle text-muted">
                      {c.min === c.max ? c.min : `${c.min}–${c.max}`}
                    </td>
                    <td className="px-5 py-3.5 align-middle">
                      {isSystemAdmin ? (
                        <select
                          className="input h-9 w-auto min-w-20 py-0 text-[13px]"
                          aria-label={`${t("templateStyle.platform")} — ${t(`templateStyle.c.${c.key}` as LocaleKey)}`}
                          value={inherited}
                          disabled={saving}
                          onChange={(e) =>
                            setPlatform({
                              ...platform,
                              [c.key]: Number(e.target.value),
                            })
                          }
                        >
                          {range(c.min, c.max).map((v) => (
                            <option key={v} value={v}>
                              {v}
                            </option>
                          ))}
                        </select>
                      ) : (
                        inherited
                      )}
                    </td>
                    <td className="px-5 py-3.5 align-middle">
                      {canEditTenant ? (
                        <select
                          className="input h-9 w-auto min-w-20 py-0 text-[13px]"
                          aria-label={`${t("templateStyle.tenant")} — ${t(`templateStyle.c.${c.key}` as LocaleKey)}`}
                          value={tenant[c.key] ?? ""}
                          disabled={saving}
                          onChange={(e) => {
                            const next = { ...tenant };
                            if (e.target.value === "") delete next[c.key];
                            else next[c.key] = Number(e.target.value);
                            setTenant(next);
                          }}
                        >
                          <option value="">
                            {t("templateStyle.follow", { size: inherited })}
                          </option>
                          {range(c.min, c.max).map((v) => (
                            <option key={v} value={v}>
                              {v}
                            </option>
                          ))}
                        </select>
                      ) : (
                        (tenant[c.key] ?? "—")
                      )}
                    </td>
                    <td className="px-5 py-3.5 align-middle font-medium">{inUse}</td>
                </>
              );
            }}
        />
      {error && <Notice tone="error">{error}</Notice>}
      {(isSystemAdmin || canEditTenant) && (
        <div className="flex flex-wrap items-center gap-3">
          {isSystemAdmin && (
            <button
              type="button"
              className="btn btn-outline btn-sm"
              disabled={saving}
              onClick={() =>
                void run(() => updatePlatformTemplateStyle(platform))
              }
            >
              {t("templateStyle.savePlatform")}
            </button>
          )}
          {canEditTenant && (
            <button
              type="button"
              className="btn btn-primary btn-sm"
              disabled={saving}
              onClick={() => void run(() => updateTenantTemplateStyle(tenant))}
            >
              {t("templateStyle.saveTenant")}
            </button>
          )}
        </div>
      )}
    </div>
  );
}
