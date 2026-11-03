/**
 * Public legal and support pages (privacy, terms, support), in the two UI
 * locales. These are documents, not UI strings, so they live here rather
 * than in the i18n dictionaries; they are also the URLs a Microsoft
 * Marketplace listing points to.
 *
 * Operator details are in LEGAL_OPERATOR — fill them in before the pages
 * go public.
 */
import type { Locale } from "./i18n";

export const LEGAL_OPERATOR = {
  // TODO(legal): the registered company name, address and contact email.
  name: "[Tên đơn vị vận hành weRAG]",
  address: "[Địa chỉ trụ sở]",
  email: "[email hỗ trợ]",
  updated: "03/11/2026",
};

export type LegalBlock = { heading: string; paragraphs?: string[]; bullets?: string[] };
export type LegalDocument = { title: string; intro: string; sections: LegalBlock[] };
export type LegalPageKey = "privacy" | "terms" | "support";

const op = LEGAL_OPERATOR;

const vi: Record<LegalPageKey, LegalDocument> = {
  privacy: {
    title: "Chính sách quyền riêng tư",
    intro: `Chính sách này giải thích weRAG (gồm trang web, ứng dụng và add-in cho Microsoft Word) thu thập, sử dụng và bảo vệ dữ liệu của bạn như thế nào. weRAG do ${op.name} vận hành.`,
    sections: [
      {
        heading: "1. Dữ liệu chúng tôi thu thập",
        bullets: [
          "Thông tin tài khoản: tên đăng nhập, email, tổ chức bạn thuộc về và vai trò trong tổ chức.",
          "Nội dung bạn cung cấp: câu hỏi, tin nhắn, tệp tải lên kho tri thức hoặc vào cuộc trò chuyện.",
          "Nội dung văn bản Word: khi bạn dùng add-in, nội dung văn bản đang mở (hoặc đoạn bạn chọn) được gửi tới máy chủ weRAG để trả lời, kiểm tra chính tả, thể thức hoặc đề xuất viết lại.",
          "Dữ liệu sử dụng: số câu hỏi, số token, số trang đã xử lý, thời điểm sử dụng — để tính hạn mức gói cước.",
          "Dữ liệu kỹ thuật: địa chỉ IP, loại trình duyệt, nhật ký lỗi — để vận hành và bảo mật dịch vụ.",
          "Thông tin thanh toán: mã đơn và nội dung chuyển khoản. Chúng tôi không nhận và không lưu số thẻ.",
        ],
      },
      {
        heading: "2. Chúng tôi dùng dữ liệu để làm gì",
        bullets: [
          "Cung cấp dịch vụ: tìm kiếm, trả lời câu hỏi kèm trích dẫn, kiểm tra và soạn thảo văn bản.",
          "Tính hạn mức, gói cước và xác nhận thanh toán.",
          "Bảo mật, phát hiện lạm dụng và khắc phục lỗi.",
          "Cải thiện chất lượng: chúng tôi có thể dùng phản hồi bạn chủ động gửi (ví dụ bấm \"Không phải lỗi\" hoặc đánh giá câu trả lời) để cải thiện mô hình của chính weRAG.",
        ],
      },
      {
        heading: "3. Xử lý bằng trí tuệ nhân tạo",
        paragraphs: [
          "Các mô hình ngôn ngữ, nhận dạng chữ (OCR) và kiểm tra chính tả của weRAG chạy trên máy chủ do chúng tôi vận hành. Nội dung tài liệu của bạn không được gửi tới nhà cung cấp AI bên thứ ba và không được bán hay chia sẻ để huấn luyện mô hình của bên khác.",
          "Khi tính năng tìm kiếm web được bật, chỉ câu truy vấn tìm kiếm (không phải tài liệu của bạn) được gửi tới dịch vụ tìm kiếm đã cấu hình.",
        ],
      },
      {
        heading: "4. Chia sẻ dữ liệu",
        bullets: [
          "Trong tổ chức của bạn: theo quyền mà quản trị viên tổ chức thiết lập (ví dụ kho tri thức được chia sẻ).",
          "Nhà cung cấp hạ tầng phục vụ việc vận hành (máy chủ, lưu trữ), theo cam kết bảo mật.",
          "Khi pháp luật yêu cầu hoặc theo yêu cầu hợp pháp của cơ quan có thẩm quyền.",
          "Chúng tôi không bán dữ liệu cá nhân.",
        ],
      },
      {
        heading: "5. Lưu trữ và bảo mật",
        paragraphs: [
          "Dữ liệu được lưu trên máy chủ đặt tại Việt Nam, truyền qua kết nối mã hóa HTTPS, phân quyền truy cập theo tổ chức và vai trò, và được sao lưu định kỳ.",
        ],
      },
      {
        heading: "6. Thời gian lưu giữ",
        paragraphs: [
          "Dữ liệu được giữ trong thời gian tài khoản còn hoạt động. Khi bạn xóa cuộc trò chuyện, tệp hoặc yêu cầu xóa tài khoản, dữ liệu được xóa khỏi hệ thống; các bản sao lưu cũ tự hết hạn theo chu kỳ sao lưu.",
        ],
      },
      {
        heading: "7. Quyền của bạn",
        paragraphs: [
          `Theo pháp luật Việt Nam về bảo vệ dữ liệu cá nhân, bạn có quyền xem, sửa, xóa dữ liệu của mình, rút lại sự đồng ý và khiếu nại. Gửi yêu cầu tới ${op.email}.`,
        ],
      },
      {
        heading: "8. Add-in cho Microsoft Word",
        bullets: [
          "Add-in chỉ đọc văn bản khi bạn dùng một chức năng của nó.",
          "Add-in chỉ thay đổi văn bản khi bạn bấm nút áp dụng hoặc đồng ý trong cuộc trò chuyện.",
          "Đăng nhập qua trình duyệt bằng mã thiết bị; add-in không thấy mật khẩu của bạn.",
        ],
      },
      {
        heading: "9. Thay đổi và liên hệ",
        paragraphs: [
          "Chúng tôi sẽ thông báo trên trang này khi chính sách thay đổi.",
          `Liên hệ: ${op.name}, ${op.address}, email ${op.email}.`,
        ],
      },
    ],
  },
  terms: {
    title: "Điều khoản sử dụng",
    intro: `Khi sử dụng weRAG (trang web, ứng dụng và add-in cho Microsoft Word) do ${op.name} cung cấp, bạn đồng ý với các điều khoản dưới đây.`,
    sections: [
      {
        heading: "1. Dịch vụ",
        paragraphs: [
          "weRAG là trợ lý tra cứu, hỏi đáp và soạn thảo văn bản dựa trên kho tri thức pháp luật và tài liệu của bạn.",
        ],
      },
      {
        heading: "2. Tài khoản",
        bullets: [
          "Bạn chịu trách nhiệm giữ bí mật thông tin đăng nhập và mọi hoạt động dưới tài khoản của mình.",
          "Quản trị viên tổ chức có thể quản lý thành viên và quyền truy cập trong tổ chức.",
        ],
      },
      {
        heading: "3. Nội dung của bạn",
        paragraphs: [
          "Bạn giữ toàn bộ quyền đối với tài liệu và nội dung bạn đưa vào weRAG. Bạn cho phép chúng tôi xử lý nội dung đó trong phạm vi cần thiết để cung cấp dịch vụ, như mô tả trong Chính sách quyền riêng tư.",
          "Bạn cam kết có quyền hợp pháp với nội dung tải lên và không tải lên nội dung vi phạm pháp luật.",
        ],
      },
      {
        heading: "4. Kết quả do AI tạo ra",
        paragraphs: [
          "Câu trả lời, đề xuất và kết quả kiểm tra do AI tạo ra chỉ mang tính tham khảo, có thể chưa đầy đủ hoặc chưa chính xác, và không thay thế ý kiến tư vấn pháp lý chuyên nghiệp. Hãy đối chiếu với văn bản gốc được trích dẫn trước khi sử dụng.",
        ],
      },
      {
        heading: "5. Sử dụng được phép",
        bullets: [
          "Không dùng dịch vụ cho mục đích vi phạm pháp luật.",
          "Không tìm cách truy cập trái phép, gây quá tải hoặc làm gián đoạn hệ thống.",
          "Không khai thác tự động hàng loạt dữ liệu ngoài các giao diện được cung cấp.",
        ],
      },
      {
        heading: "6. Gói cước và thanh toán",
        paragraphs: [
          "Một số tính năng cần gói cước trả phí. Giá, hạn mức và thời hạn của từng gói được công bố trên trang gói cước. Thanh toán bằng chuyển khoản ngân hàng; gói được kích hoạt sau khi khoản thanh toán được xác nhận. Add-in cho Word có thể yêu cầu mua thêm gói cước.",
        ],
      },
      {
        heading: "7. Tạm ngừng và chấm dứt",
        paragraphs: [
          "Chúng tôi có thể tạm khóa tài khoản vi phạm các điều khoản này. Bạn có thể ngừng sử dụng và yêu cầu xóa tài khoản bất cứ lúc nào.",
        ],
      },
      {
        heading: "8. Giới hạn trách nhiệm",
        paragraphs: [
          "Dịch vụ được cung cấp theo hiện trạng. Trong phạm vi pháp luật cho phép, chúng tôi không chịu trách nhiệm về thiệt hại gián tiếp phát sinh từ việc sử dụng kết quả do AI tạo ra hoặc từ việc gián đoạn dịch vụ.",
        ],
      },
      {
        heading: "9. Luật áp dụng và liên hệ",
        paragraphs: [
          "Các điều khoản này được điều chỉnh bởi pháp luật Việt Nam. Chúng tôi sẽ thông báo trên trang này khi điều khoản thay đổi.",
          `Liên hệ: ${op.name}, ${op.address}, email ${op.email}.`,
        ],
      },
    ],
  },
  support: {
    title: "Hỗ trợ",
    intro: `Cần giúp đỡ với weRAG hoặc add-in cho Microsoft Word? Gửi email tới ${op.email}, nêu tài khoản đăng nhập và mô tả vấn đề; chúng tôi phản hồi trong giờ làm việc.`,
    sections: [
      {
        heading: "Cài add-in cho Word",
        bullets: [
          "Từ kho add-in: trong Word chọn Insert → Get Add-ins (hoặc Home → Add-ins), tìm \"weRAG\" và bấm Add.",
          "Cách khác (Windows, macOS, Word trên web): xem mục \"Cài đặt add-in Word\" trên trang đăng nhập.",
          "Yêu cầu: Word 2019 trở lên (Windows hoặc macOS), Microsoft 365, hoặc Word trên web.",
        ],
      },
      {
        heading: "Đăng nhập trong add-in",
        bullets: [
          "Mở add-in, bấm đăng nhập: add-in hiện một mã và mở trang đăng nhập trên trình duyệt.",
          "Đăng nhập bằng tài khoản weRAG, xác nhận mã; add-in tự đăng nhập.",
          "Chưa có tài khoản? Đăng ký trên trang web weRAG.",
        ],
      },
      {
        heading: "Câu hỏi thường gặp",
        bullets: [
          "Hết hạn mức: xem gói cước trong mục Cài đặt → Gói & thanh toán → Gói sử dụng, hoặc liên hệ quản trị viên tổ chức.",
          "Add-in không sửa văn bản của tôi? Đúng vậy: add-in chỉ đánh dấu và đề xuất, văn bản chỉ thay đổi khi bạn bấm áp dụng.",
          "Gỡ add-in: trong Word mở My Add-ins, chọn weRAG và bấm Remove.",
        ],
      },
    ],
  },
};

const en: Record<LegalPageKey, LegalDocument> = {
  privacy: {
    title: "Privacy policy",
    intro: `This policy explains how weRAG (the website, the app and the add-in for Microsoft Word) collects, uses and protects your data. weRAG is operated by ${op.name}.`,
    sections: [
      {
        heading: "1. Data we collect",
        bullets: [
          "Account information: user name, email, the organisation you belong to and your role in it.",
          "Content you provide: questions, messages, and files uploaded to a knowledge base or a conversation.",
          "Word document content: when you use the add-in, the text of the open document (or the passage you selected) is sent to the weRAG server to answer, check spelling or formatting, or suggest rewrites.",
          "Usage data: number of questions, tokens, processed pages and times of use — to meter plan quotas.",
          "Technical data: IP address, browser type and error logs — to run and secure the service.",
          "Payment information: order codes and bank-transfer references. We never receive or store card numbers.",
        ],
      },
      {
        heading: "2. How we use data",
        bullets: [
          "To provide the service: search, cited answers, document checks and drafting.",
          "To meter quotas, manage plans and confirm payments.",
          "For security, abuse detection and troubleshooting.",
          "To improve quality: feedback you choose to give (for example \"Not an error\" or rating an answer) may be used to improve weRAG's own models.",
        ],
      },
      {
        heading: "3. AI processing",
        paragraphs: [
          "weRAG's language, OCR and spelling models run on servers we operate. Your document content is not sent to third-party AI providers and is never sold or shared to train other parties' models.",
          "When web search is enabled, only the search query (not your documents) is sent to the configured search service.",
        ],
      },
      {
        heading: "4. Sharing",
        bullets: [
          "Within your organisation, according to the permissions its administrators set (for example shared knowledge bases).",
          "With infrastructure providers that help run the service (servers, storage), under confidentiality obligations.",
          "When required by law or a lawful request from a competent authority.",
          "We do not sell personal data.",
        ],
      },
      {
        heading: "5. Storage and security",
        paragraphs: [
          "Data is stored on servers located in Vietnam, sent over encrypted HTTPS connections, protected by organisation- and role-based access control, and backed up regularly.",
        ],
      },
      {
        heading: "6. Retention",
        paragraphs: [
          "Data is kept while your account is active. When you delete a conversation or file, or ask us to delete your account, the data is removed from the system; older backups expire on the backup schedule.",
        ],
      },
      {
        heading: "7. Your rights",
        paragraphs: [
          `Under Vietnamese personal data protection law you may access, correct and delete your data, withdraw consent and lodge a complaint. Send requests to ${op.email}.`,
        ],
      },
      {
        heading: "8. The Microsoft Word add-in",
        bullets: [
          "The add-in reads the document only when you use one of its features.",
          "The add-in changes the document only when you click an apply button or agree in the conversation.",
          "Sign-in happens in your browser with a device code; the add-in never sees your password.",
        ],
      },
      {
        heading: "9. Changes and contact",
        paragraphs: [
          "We will announce changes to this policy on this page.",
          `Contact: ${op.name}, ${op.address}, email ${op.email}.`,
        ],
      },
    ],
  },
  terms: {
    title: "Terms of use",
    intro: `By using weRAG (the website, the app and the add-in for Microsoft Word), provided by ${op.name}, you agree to the terms below.`,
    sections: [
      {
        heading: "1. The service",
        paragraphs: [
          "weRAG is an assistant for looking up, asking about and drafting documents, grounded in a legal knowledge base and your own documents.",
        ],
      },
      {
        heading: "2. Accounts",
        bullets: [
          "You are responsible for keeping your sign-in details secret and for all activity under your account.",
          "Organisation administrators may manage members and access within their organisation.",
        ],
      },
      {
        heading: "3. Your content",
        paragraphs: [
          "You keep all rights to the documents and content you bring into weRAG. You allow us to process that content as needed to provide the service, as described in the Privacy policy.",
          "You confirm you have the right to upload your content and will not upload unlawful content.",
        ],
      },
      {
        heading: "4. AI-generated output",
        paragraphs: [
          "Answers, suggestions and check results generated by AI are for reference only, may be incomplete or inaccurate, and are not a substitute for professional legal advice. Check them against the cited source documents before relying on them.",
        ],
      },
      {
        heading: "5. Acceptable use",
        bullets: [
          "Do not use the service for unlawful purposes.",
          "Do not attempt unauthorised access, overload or disrupt the system.",
          "Do not bulk-harvest data outside the interfaces provided.",
        ],
      },
      {
        heading: "6. Plans and payment",
        paragraphs: [
          "Some features require a paid plan. Prices, quotas and durations are published on the pricing page. Payment is by bank transfer; a plan is activated once the payment is confirmed. The Word add-in may require an additional purchase.",
        ],
      },
      {
        heading: "7. Suspension and termination",
        paragraphs: [
          "We may suspend accounts that breach these terms. You may stop using the service and ask us to delete your account at any time.",
        ],
      },
      {
        heading: "8. Limitation of liability",
        paragraphs: [
          "The service is provided as is. To the extent permitted by law, we are not liable for indirect damages arising from the use of AI-generated output or from service interruptions.",
        ],
      },
      {
        heading: "9. Governing law and contact",
        paragraphs: [
          "These terms are governed by the laws of Vietnam. We will announce changes on this page.",
          `Contact: ${op.name}, ${op.address}, email ${op.email}.`,
        ],
      },
    ],
  },
  support: {
    title: "Support",
    intro: `Need help with weRAG or the add-in for Microsoft Word? Email ${op.email} with your sign-in name and a description of the problem; we reply during business hours.`,
    sections: [
      {
        heading: "Install the Word add-in",
        bullets: [
          "From the add-in store: in Word choose Insert → Get Add-ins (or Home → Add-ins), search for \"weRAG\" and click Add.",
          "Other ways (Windows, macOS, Word on the web): see \"Install the Word add-in\" on the sign-in page.",
          "Requirements: Word 2019 or later (Windows or macOS), Microsoft 365, or Word on the web.",
        ],
      },
      {
        heading: "Signing in to the add-in",
        bullets: [
          "Open the add-in and click sign in: it shows a code and opens the sign-in page in your browser.",
          "Sign in with your weRAG account and confirm the code; the add-in signs in by itself.",
          "No account yet? Sign up on the weRAG website.",
        ],
      },
      {
        heading: "Frequently asked questions",
        bullets: [
          "Out of quota: see your plan under Settings → Plan & billing → Usage package, or contact your organisation administrator.",
          "The add-in does not edit my document? By design: it marks and suggests, and the document changes only when you click apply.",
          "Remove the add-in: in Word open My Add-ins, choose weRAG and click Remove.",
        ],
      },
    ],
  },
};

export function legalDocument(page: LegalPageKey, locale: Locale): LegalDocument {
  return (locale === "en" ? en : vi)[page];
}
