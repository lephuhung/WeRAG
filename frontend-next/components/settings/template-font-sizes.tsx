/* Cỡ chữ và lề văn bản mẫu: font sizes (pt) and page margins (mm) of the
 * NĐ30 templates drafted from chat.
 * The SuperAdmin edits the platform default column, the workspace admin the
 * unit's override column ("follow" leaves a row to the platform); everyone
 * else reads the sizes in use. The server checks the NĐ30 ranges. */
"use client";

import { Fragment, useCallback, useEffect, useState } from "react";
import {
  getTemplateStyle,
  updatePlatformTemplateStyle,
  updateTenantTemplateStyle,
  type TemplateFontSizes,
  type TemplateStyle,
} from "@/lib/api/template-style";
import { useT, type LocaleKey } from "@/lib/i18n";

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
  const [style, setStyle] = useState<TemplateStyle | null>(null);
  const [platform, setPlatform] = useState<TemplateFontSizes>({});
  const [tenant, setTenant] = useState<TemplateFontSizes>({});
  const [saving, setSaving] = useState(false);
  const [message, setMessage] = useState<{ ok: boolean; text: string } | null>(
    null,
  );

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
      setMessage({
        ok: false,
        text: e instanceof Error ? e.message : String(e),
      });
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const run = async (save: () => Promise<unknown>) => {
    setSaving(true);
    setMessage(null);
    try {
      await save();
      await load();
      setMessage({ ok: true, text: t("templateStyle.saved") });
    } catch (e) {
      setMessage({
        ok: false,
        text: t("templateStyle.saveFailed", {
          error: e instanceof Error ? e.message : String(e),
        }),
      });
    } finally {
      setSaving(false);
    }
  };

  if (!style)
    return message ? (
      <p className="caption text-error">{message.text}</p>
    ) : null;

  const range = (min: number, max: number) =>
    Array.from({ length: max - min + 1 }, (_, i) => min + i);

  return (
    <div className="flex flex-col gap-3 border-t border-hairline pt-4">
      <div>
        <h2 className="text-base font-semibold">{t("templateStyle.title")}</h2>
        <p className="caption mt-1 text-muted">{t("templateStyle.desc")}</p>
      </div>
      <div className="overflow-x-auto">
        <table className="w-full text-sm">
          <thead>
            <tr className="caption text-left text-muted">
              <th className="py-1.5 pr-3 font-normal">
                {t("templateStyle.component")}
              </th>
              <th className="py-1.5 pr-3 font-normal">
                {t("templateStyle.range")}
              </th>
              <th className="py-1.5 pr-3 font-normal">
                {t("templateStyle.platform")}
              </th>
              <th className="py-1.5 pr-3 font-normal">
                {t("templateStyle.tenant")}
              </th>
              <th className="py-1.5 font-normal">
                {t("templateStyle.effective")}
              </th>
            </tr>
          </thead>
          <tbody>
            {style.components.map((c, i) => {
              const inherited = platform[c.key] ?? c.default;
              const inUse = tenant[c.key] ?? inherited;
              return (
                <Fragment key={c.key}>
                  {c.unit !== style.components[i - 1]?.unit && (
                    <tr>
                      <td
                        colSpan={5}
                        className="pb-1 pt-3 text-[13px] font-medium"
                      >
                        {t(
                          c.unit === "mm"
                            ? "templateStyle.groupMargins"
                            : "templateStyle.groupFonts",
                        )}
                      </td>
                    </tr>
                  )}
                  <tr className="border-t border-hairline">
                    <td className="py-1.5 pr-3">
                      {t(`templateStyle.c.${c.key}` as LocaleKey)}
                      {EXAMPLES[c.key] && (
                        <span className="caption block text-muted">
                          {t("templateStyle.example", {
                            text: EXAMPLES[c.key],
                          })}
                        </span>
                      )}
                    </td>
                    <td className="caption py-1.5 pr-3 text-muted">
                      {c.min === c.max ? c.min : `${c.min}–${c.max}`}
                    </td>
                    <td className="py-1.5 pr-3">
                      {isSystemAdmin ? (
                        <select
                          className="input h-9 w-auto min-w-20 py-0 text-[13px]"
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
                    <td className="py-1.5 pr-3">
                      {canEditTenant ? (
                        <select
                          className="input h-9 w-auto min-w-20 py-0 text-[13px]"
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
                    <td className="py-1.5 font-medium">{inUse}</td>
                  </tr>
                </Fragment>
              );
            })}
          </tbody>
        </table>
      </div>
      {message && (
        <p className={`caption ${message.ok ? "text-muted" : "text-error"}`}>
          {message.text}
        </p>
      )}
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
