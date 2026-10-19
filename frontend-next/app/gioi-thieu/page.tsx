import type { Metadata } from "next";
import Link from "next/link";
import { BrandLogo } from "@/components/brand-logo";
import { LandingPricing } from "./pricing";
import styles from "./page.module.css";

export const metadata: Metadata = {
  title: "weRAG — Trợ lý pháp luật",
  description: "Khám phá weRAG: tra cứu, hỏi đáp văn bản pháp luật có nguồn trích dẫn cho cá nhân, đội nhóm và tổ chức.",
};

const features = [
  ["01", "Hỏi đúng tài liệu. Tìm đúng câu trả lời.", "Trò chuyện với kho tri thức bằng ngôn ngữ tự nhiên. Theo dõi nguồn tham chiếu để đọc lại nội dung gốc.", "Hỏi đáp & tra cứu"],
  ["02", "Tri thức tập trung, công việc liền mạch.", "Tổ chức tài liệu thành các kho tri thức và chia sẻ trong không gian làm việc của đội nhóm.", "Kho tri thức"],
  ["03", "Một trợ lý cho từng chuyên môn.", "Cấu hình AI Agent với hướng dẫn và nguồn tri thức phù hợp cho từng nhu cầu công việc.", "AI Agent"],
  ["04", "Từ tìm hiểu đến hoàn thiện tài liệu.", "Khai thác nội dung, làm việc với tài liệu và kết nối trợ lý vào quy trình soạn thảo.", "Trợ lý tài liệu"],
  ["05", "Kết nối với cách bạn đang làm việc.", "Mở rộng khả năng của trợ lý qua các tích hợp, công cụ và API của hệ thống.", "Tích hợp & API"],
  ["06", "Chủ động quản lý việc sử dụng.", "Quản lý thành viên, phân quyền và theo dõi hạn mức theo cá nhân hoặc không gian làm việc.", "Quản trị & hạn mức"],
];

export default function MarketingLandingPage() {
  return <div className={styles.page} lang="vi">
    <a href="#noi-dung" className={styles.skip}>Đến nội dung chính</a>
    <header className={styles.header}>
      <Link href="/" className={styles.brand} aria-label="weRAG — Trợ lý pháp luật — Trang chủ"><BrandLogo size={38} priority /><span className={styles.brandText}><strong>weRAG</strong><small>Trợ lý pháp luật</small></span></Link>
      <nav aria-label="Điều hướng chính"><a href="#tinh-nang">Tính năng</a><a href="#cach-hoat-dong">Cách hoạt động</a><a href="#goi-cuoc">Gói cước</a></nav>
      <div className={styles.headerActions}><Link href="/login" className={styles.login}>Đăng nhập</Link><Link href="/platform/creatChat" className={styles.primary}>Vào ứng dụng <span aria-hidden="true">↗</span></Link></div>
    </header>
    <main id="noi-dung">
      <section className={styles.hero}>
        <div className={styles.heroCopy}><p className={styles.eyebrow}><span /> KHÔNG GIAN TRI THỨC THÔNG MINH</p>
          <h1>Tri thức của bạn.<br /><em>Sức mạnh của AI.</em></h1>
          <p className={styles.intro}>Biến tài liệu thành câu trả lời hữu ích. WeRAG giúp bạn và đội nhóm tìm kiếm, thấu hiểu và khai thác tri thức trong một không gian làm việc.</p>
          <div className={styles.actions}><Link href="/register" className={styles.primary}>Bắt đầu với WeRAG <span aria-hidden="true">↗</span></Link><a href="#goi-cuoc" className={styles.secondary}>Khám phá các gói <span aria-hidden="true">↓</span></a></div>
          <p className={styles.heroNote}>Dành cho cá nhân, đội nhóm và tổ chức.</p>
        </div>
        <div className={styles.preview} aria-label="Ví dụ minh họa hỏi đáp tài liệu">
          <div className={styles.previewTop}><BrandLogo size={28} /><span>Trợ lý pháp luật</span><span className={styles.demoLabel}>MINH HỌA</span></div>
          <div className={styles.previewBody}><p className={styles.smallLabel}>NGUỒN TRI THỨC CỦA BẠN</p><div className={styles.files}><span>▤ &nbsp; Quy trình nội bộ.pdf</span><span>▤ &nbsp; Hướng dẫn sản phẩm.docx</span></div>
            <div className={styles.question}>Tóm tắt những điểm chính trong tài liệu này.</div>
            <div className={styles.answer}><span className={styles.answerIcon}>✳</span><div><strong>Từ thông tin đến hiểu biết.</strong><p>Đặt câu hỏi, khám phá nội dung và quay lại nguồn tham chiếu ngay trong cuộc trò chuyện.</p><span className={styles.source}>↗ Nguồn tham chiếu · Tài liệu gốc</span></div></div>
            <div className={styles.prompt}>Bạn muốn tìm hiểu điều gì? <span aria-hidden="true">↑</span></div>
          </div><div className={styles.previewFoot}><span /> Tri thức được kết nối. Ý tưởng được mở ra.</div>
        </div>
      </section>
      <div className={styles.strip}><span>TỪ DỮ LIỆU ĐẾN GIÁ TRỊ</span><p>Tìm kiếm có ngữ cảnh <b>✳</b> Câu trả lời có nguồn <b>✳</b> Cộng tác cùng AI</p></div>
      <section id="tinh-nang" className={styles.section}>
        <p className={styles.eyebrow}>ÍT THỜI GIAN TÌM KIẾM. NHIỀU KHÔNG GIAN SUY NGHĨ.</p>
        <div className={styles.sectionHead}><h2>Mọi tri thức.<br /><em>Một điểm kết nối.</em></h2><p>Từ một tài liệu đến kho kiến thức của cả tổ chức, WeRAG đưa thông tin vào đúng ngữ cảnh công việc.</p></div>
        <div className={styles.features}>{features.map(([number, title, description, label]) => <article key={number}><div className={styles.featureTop}><span>{number}</span><span>{label}</span></div><h3>{title}</h3><p>{description}</p></article>)}</div>
      </section>
      <section id="cach-hoat-dong" className={`${styles.section} ${styles.workflow}`}><p className={styles.eyebrow}>BẮT ĐẦU THẬT TỰ NHIÊN</p><h2>Tài liệu của bạn.<br /><em>Một cách làm việc mới.</em></h2><div className={styles.steps}>{[["01", "Xây dựng kho tri thức", "Đưa tài liệu vào hệ thống và tổ chức theo chủ đề hoặc dự án."], ["02", "Đặt câu hỏi cho AI", "Chọn nguồn tri thức, trò chuyện và khám phá nội dung liên quan."], ["03", "Đưa hiểu biết vào công việc", "Kiểm tra nguồn tham chiếu, tổng hợp thông tin và cộng tác cùng đội nhóm."]].map(([n,t,d]) => <article key={n}><span>{n} /</span><h3>{t}</h3><p>{d}</p></article>)}</div></section>
      <section id="goi-cuoc" className={styles.section}><p className={styles.eyebrow}>LINH HOẠT THEO NHU CẦU</p><div className={styles.sectionHead}><h2>Chọn cách bạn<br /><em>khai thác tri thức.</em></h2><p>Khám phá hình thức sử dụng phù hợp. Giá và hạn mức cụ thể được hiển thị theo các gói đang mở bán trong hệ thống.</p></div><LandingPricing /></section>
      <section className={`${styles.section} ${styles.faq}`}><p className={styles.eyebrow}>GIẢI ĐÁP TRƯỚC KHI BẮT ĐẦU</p><h2>Bạn có thể muốn biết.</h2>{[["WeRAG giúp tôi làm được gì?", "Bạn có thể tổ chức kho tri thức, hỏi đáp trên tài liệu, tạo trợ lý AI chuyên biệt và sử dụng các tích hợp phục vụ công việc."], ["Làm thế nào để xem giá và mua gói?", "Bạn có thể xem giá và hạn mức các gói đang mở bán ngay trên trang này mà không cần đăng nhập. Khi chọn mua, hãy đăng nhập vào ứng dụng để xác nhận thời hạn và tổng tiền."], ["Đội nhóm có thể dùng chung gói không?", "Hệ thống hỗ trợ gói cho không gian làm việc, với hạn mức dùng chung hoặc theo số chỗ tùy cấu hình của từng gói."], ["Câu trả lời có nguồn tham chiếu không?", "Khi tra cứu kho tri thức, bạn có thể theo dõi các nguồn được truy xuất để đối chiếu với tài liệu gốc. Hãy kiểm tra nội dung nguồn trước khi sử dụng thông tin quan trọng."]].map(([q,a]) => <details key={q}><summary>{q}<span aria-hidden="true">+</span></summary><p>{a}</p></details>)}</section>
      <section className={styles.cta}><p className={styles.eyebrow}>BẮT ĐẦU TỪ TRI THỨC BẠN ĐÃ CÓ</p><h2>Câu trả lời tiếp theo<br />nằm trong tài liệu của bạn.</h2><Link href="/register" className={styles.primary}>Tạo tài khoản WeRAG <span aria-hidden="true">↗</span></Link></section>
    </main>
    <footer className={styles.footer}><Link href="/" className={styles.brand}><BrandLogo size={38} /><span className={styles.brandText}><strong>weRAG</strong><small>Trợ lý pháp luật</small></span></Link><span>Tri thức được kết nối.</span><div><a href="#goi-cuoc">Gói cước</a><Link href="/platform/creatChat">Vào ứng dụng ↗</Link></div></footer>
  </div>;
}
