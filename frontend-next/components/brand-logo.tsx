import Image from "next/image";

interface BrandLogoProps {
  size?: number;
  className?: string;
  priority?: boolean;
}

/**
 * BrandLogo — Biểu tượng Logo đại diện cho hệ thống Tra cứu tài liệu (WeRAG).
 * Theo yêu cầu: Logo chỉ gồm ảnh/icon biểu tượng, không kèm text title.
 */
export function BrandLogo({ size = 36, className = "", priority = false }: BrandLogoProps) {
  return (
    <div
      className={`relative inline-flex items-center justify-center shrink-0 overflow-hidden rounded-xl shadow-sm ${className}`}
      style={{ width: size, height: size }}
    >
      <Image
        src="/logo.png"
        alt="Logo Tra cứu tài liệu"
        width={size}
        height={size}
        priority={priority}
        className="h-full w-full object-cover"
      />
    </div>
  );
}

export default BrandLogo;
