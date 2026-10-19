"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import { listPublicPlans, planPrice, type PublicUsagePlan } from "@/lib/api/usage";
import styles from "./page.module.css";

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
          <ul>{(plan.limits ?? []).map((limit, i) => <li key={i}>
            {limit.limit === 0 ? "Không giới hạn" : limit.limit.toLocaleString("vi-VN")} {limit.metric === "questions" ? "câu hỏi" : "token"} / {limit.period === "day" ? "ngày" : "tháng"}
          </li>)}</ul>
          <Link href="/platform/creatChat" className={styles.secondary}>Chọn gói trong ứng dụng ↗</Link>
        </article>)}</div>}
    </div>
    <p className={styles.pricingNote}>Giá và hạn mức được cập nhật từ các gói đang mở bán. Bạn chỉ cần đăng nhập khi chọn mua gói.</p>
  </div>;
}
