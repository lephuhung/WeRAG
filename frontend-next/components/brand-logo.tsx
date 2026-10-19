import Image from "next/image";

interface BrandLogoProps {
  size?: number;
  className?: string;
  priority?: boolean;
}

/**
 * Shared open-book mark for weRAG — Trợ lý pháp luật.
 */
export function BrandLogo({ size = 36, className = "", priority = false }: BrandLogoProps) {
  return (
    <div
      className={`relative inline-flex shrink-0 items-center justify-center ${className}`}
      style={{ width: size, height: size }}
    >
      <Image
        src="/brand-mark.svg"
        alt="weRAG — Trợ lý pháp luật"
        width={size}
        height={size}
        preload={priority}
        className="h-full w-full object-contain"
      />
    </div>
  );
}

export default BrandLogo;
