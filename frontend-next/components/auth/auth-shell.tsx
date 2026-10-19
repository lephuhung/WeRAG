import Link from "next/link";
import type { ReactNode } from "react";
import { BrandLogo } from "@/components/brand-logo";
import styles from "./auth-shell.module.css";

export function AuthShell({ children, mode }: { children: ReactNode; mode: "login" | "register" }) {
  return (
    <main className={styles.shell}>
      <section className={styles.showcase} aria-label="Giới thiệu WeRAG">
        <Link href="/" className={styles.brand} aria-label="WeRAG — Về trang chủ">
          <BrandLogo size={44} priority />
          <div><strong>WeRAG<span> / </span></strong><small>TRỢ LÝ TRI THỨC PHÁP LUẬT</small></div>
        </Link>
        <div className={styles.story}>
          <p className={styles.eyebrow}><span /> KẾT NỐI TRI THỨC · LÀM RÕ CĂN CỨ</p>
          <h1>Hiểu đúng văn bản.<br /><em>Tra cứu có căn cứ.</em></h1>
          <p className={styles.description}>Từ những trang văn bản đến câu trả lời bạn cần. Khám phá, tra cứu và hỏi đáp pháp luật cùng trợ lý AI, với nguồn tài liệu để đối chiếu.</p>
          <div className={styles.illustration} role="img" aria-label="Minh họa WeRAG kết nối văn bản pháp luật với câu trả lời có nguồn trích dẫn">
            <div className={styles.orbit} />
            <div className={styles.document}>
              <div className={styles.docTop}><span>§</span><span>THƯ VIỆN PHÁP LUẬT<br /><small>TÀI LIỆU THAM CHIẾU</small></span></div>
              <div className={styles.docRule} />
              <h2>Văn bản pháp luật</h2>
              <p>Luật · Nghị định · Thông tư</p>
              <div className={styles.lines}><i /><i /><i /><i /></div>
              <div className={styles.docHighlight}>Điều khoản & căn cứ liên quan</div>
              <div className={styles.lines}><i /><i /><i /></div>
            </div>
            <div className={styles.question}><span>⌕</span> Tìm căn cứ cho vấn đề của bạn</div>
            <div className={styles.answer}>
              <div className={styles.answerTop}><span className={styles.sparkle}>✦</span><strong>Câu trả lời từ tri thức</strong><span className={styles.ai}>AI</span></div>
              <p>Tổng hợp thông tin liên quan từ tài liệu, kèm trích dẫn để bạn tiếp tục kiểm chứng.</p>
              <div className={styles.citations}><span>↗ Văn bản gốc</span><span>§ Điều khoản liên quan</span></div>
            </div>
            <span className={styles.demoLabel}>MINH HỌA KHẢ NĂNG HỆ THỐNG</span>
          </div>
          <div className={styles.wordAddin}>
            <span className={styles.wordIcon} aria-hidden="true">W</span>
            <div>
              <span className={styles.wordLabel}>ADD-IN CHO MICROSOFT WORD</span>
              <strong>WeRAG ngay trong Microsoft Word</strong>
              <p>Tra cứu và hỏi đáp tài liệu ngay bên cạnh văn bản đang soạn thảo.</p>
            </div>
            <details className={styles.wordInstall}>
              <summary>Cài đặt add-in Word <span aria-hidden="true">↗</span></summary>
              <div className={styles.installGuide}>
                <strong>Cài WeRAG trong Microsoft Word</strong>
                <a className={styles.manifestDownload} href="/word-addin/manifest.xml" download="werag-word-addin.xml">↓ Tải manifest cho Word <span>.xml</span></a>
                <ol>
                  <li><b>Tải file manifest.</b> Lưu file <b>werag-word-addin.xml</b> vào thư mục dùng chung đã được cơ quan cấu hình cho add-in.</li>
                  <li><b>Mở Word trên máy tính.</b> Chọn <b>Insert (Chèn) → My Add-ins (Phần bổ trợ của tôi)</b>.</li>
                  <li><b>Thêm WeRAG.</b> Mở thẻ <b>Shared Folder (Thư mục dùng chung)</b>, chọn <b>WeRAG soạn thảo</b> rồi nhấn <b>Add (Thêm)</b>.</li>
                  <li><b>Đăng nhập và sử dụng.</b> Mở WeRAG từ thẻ <b>Home (Trang đầu)</b>, làm theo hướng dẫn đăng nhập trong add-in để bắt đầu tra cứu, hỏi đáp.</li>
                </ol>
                <p>Chưa có danh mục add-in? Trong Word, mở <b>File → Options → Trust Center → Trust Center Settings → Trusted Add-in Catalogs</b>, thêm đường dẫn thư mục chia sẻ do quản trị viên cung cấp, bật <b>Show in Menu</b> rồi khởi động lại Word. File XML không cài bằng cách nhấp đúp. Áp dụng cho Word 2021 trở lên trên Windows.</p>
              </div>
            </details>
          </div>
          <div className={styles.features}>
            <div><span>01 /</span><strong>Tra cứu thông minh</strong><p>Tìm theo từ khóa và ngữ nghĩa</p></div>
            <div><span>02 /</span><strong>Hỏi đáp tài liệu</strong><p>Đặt câu hỏi bằng ngôn ngữ tự nhiên</p></div>
            <div><span>03 /</span><strong>Đối chiếu nguồn</strong><p>Theo dấu căn cứ trong văn bản</p></div>
          </div>
        </div>
        <footer className={styles.showcaseFooter}><span>TRI THỨC RÕ RÀNG. CÔNG VIỆC THÔNG SUỐT.</span><span>WeRAG © {new Date().getFullYear()}</span></footer>
      </section>
      <section className={styles.formSide} aria-label={mode === "login" ? "Đăng nhập" : "Đăng ký"}>
        <div className={styles.topLink}>{mode === "login" ? "Bạn chưa có tài khoản?" : "Bạn đã có tài khoản?"} <Link href={mode === "login" ? "/register" : "/login"}>{mode === "login" ? "Đăng ký" : "Đăng nhập"} <span aria-hidden="true">↗</span></Link></div>
        <div className={styles.formContent}>{children}</div>
        <p className={styles.formFooter}>WeRAG · Không gian tri thức pháp luật của bạn</p>
      </section>
    </main>
  );
}

export { styles as authStyles };
