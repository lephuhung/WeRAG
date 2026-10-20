"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import { formatPriceVND, listPublicPlans, planPrice, type PublicUsagePlan, type UsagePlanCaps } from "@/lib/api/usage";
import styles from "./page.module.css";

const num = (n: number) => n.toLocaleString("vi-VN");

/* What a plan allows beyond its day/month quota, one line per cap it sets. */
function capLines(caps: UsagePlanCaps | undefined, mode: PublicUsagePlan["mode"], scope: "user" | "tenant"): string[] {
  const lines: string[] = [];
  if (scope === "tenant") lines.push(mode === "seat" ? "Mỗi thành viên được cấp chỗ có hạn mức riêng" : "Cả tổ chức dùng chung một hạn mức");
  const c = caps ?? {};
  if (c["session.max_turns"]) lines.push(`Tối đa ${num(c["session.max_turns"])} câu hỏi mỗi cuộc trò chuyện`);
  if (c["session.max_tokens"]) lines.push(`Tối đa ${num(c["session.max_tokens"])} token mỗi cuộc trò chuyện`);
  if (c["context.history_tokens"]) lines.push(`Ghi nhớ khoảng ${num(c["context.history_tokens"])} token hội thoại gần nhất`);
  if (c["agent.turn_token_cap"]) lines.push(`Tối đa ${num(c["agent.turn_token_cap"])} token cho mỗi câu trả lời`);
  if (c["agent.max_iterations"]) lines.push(`Tối đa ${num(c["agent.max_iterations"])} bước tra cứu cho mỗi câu trả lời`);
  return lines;
}

export function LandingPricing() {
  const [scope, setScope] = useState<"user" | "tenant">("user");
  const [plans, setPlans] = useState<PublicUsagePlan[]>([]);
  const [status, setStatus] = useState<"loading" | "ready" | "error">("loading");
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    const controller = new AbortController();
    setStatus("loading");
    listPublicPlans(scope, controller.signal).then(result => {
      if (!controller.signal.aborted) {
        setPlans(result);
        setStatus("ready");
      }
    }).catch(() => {
      if (!controller.signal.aborted) setStatus("error");
    });
    return () => controller.abort();
  }, [scope, attempt]);

  return <div>
    <div className={styles.scope} role="group" aria-label="Đối tượng sử dụng">
      <button type="button" aria-pressed={scope === "user"} onClick={() => setScope("user")}>Cá nhân</button>
      <button type="button" aria-pressed={scope === "tenant"} onClick={() => setScope("tenant")}>Tổ chức</button>
    </div>
    <div aria-live="polite" aria-busy={status === "loading"}>
      {status === "loading" ? <p className={styles.pricingNote}>Đang tải các gói đang mở bán…</p>
        : status === "error" ? <div className={styles.pricingNote}><p>Chưa tải được bảng giá. Vui lòng thử lại.</p><button type="button" className={styles.secondary} onClick={() => setAttempt(value => value + 1)}>Tải lại bảng giá ↻</button></div>
        : plans.length === 0 ? <p className={styles.pricingNote}>Hiện chưa có gói mở bán cho lựa chọn này. Vui lòng liên hệ quản trị viên để được hỗ trợ.</p>
        : <div className={styles.planGrid}>{plans.map(plan => <article key={plan.id} className={styles.plan}>
          <p className={styles.eyebrow}>{plan.mode === "seat" ? "THEO SỐ CHỖ" : scope === "user" ? "CÁ NHÂN" : "HẠN MỨC DÙNG CHUNG"}</p>
          <h3>{plan.name}</h3><p>{plan.description}</p>
          <div className={styles.price}>{planPrice(plan)}<small> / {plan.mode === "seat" ? "chỗ / " : ""}tháng</small></div>
          <div className={styles.planDetails}>
          <ul className={styles.planLimits}>{(plan.limits ?? []).map((limit, i) => <li key={i}>
            {limit.limit === 0 ? "Không giới hạn" : limit.limit.toLocaleString("vi-VN")} {limit.metric === "questions" ? "câu hỏi" : "token"} / {limit.period === "day" ? "ngày" : "tháng"}
          </li>)}</ul>
          {capLines(plan.caps, plan.mode, scope).length > 0 && <div className={styles.planAgents}>
            <h4>Quyền lợi của gói</h4>
            <ul>{capLines(plan.caps, plan.mode, scope).map(line => <li key={line}>{line}</li>)}</ul>
          </div>}
          {!!plan.included_agents?.length && <div className={styles.planAgents}>
            <h4>Trợ lý đi kèm</h4>
            <ul>{plan.included_agents.map(agent => <li key={agent.id}>{agent.name}</li>)}</ul>
          </div>}
          {!!plan.addons?.length && <div className={styles.planAddons}>
            <h4>Trợ lý chuyên ngành mua thêm</h4>
            <ul>{plan.addons.map(addon => <li key={addon.agent_id}>
              <strong>{addon.name}</strong>
              {addon.description && <p>{addon.description}</p>}
              <span>{addon.price_vnd > 0 ? `+${formatPriceVND(addon.price_vnd)}` : "Không tính thêm phí"}{addon.price_vnd > 0 && (addon.per_seat ? " / chỗ / tháng" : " / tháng")}</span>
            </li>)}</ul>
          </div>}
          </div>
          <Link href="/platform/creatChat" className={styles.secondary}>Chọn gói trong ứng dụng ↗</Link>
        </article>)}</div>}
    </div>
    <p className={styles.pricingNote}>Giá và hạn mức được cập nhật từ các gói đang mở bán. Bạn chỉ cần đăng nhập khi chọn mua gói.</p>
  </div>;
}
