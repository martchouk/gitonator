import { useState } from 'react';

const FallbackSvg = ({ size }: { size: number }) => (
  <svg width={size} height={size} viewBox="0 0 32 32" fill="none" xmlns="http://www.w3.org/2000/svg">
    <defs>
      <linearGradient id="logo-g" x1="0.15" y1="0" x2="0.85" y2="1">
        <stop offset="0%" stopColor="#818cf8" />
        <stop offset="100%" stopColor="#22d3ee" />
      </linearGradient>
    </defs>
    <circle cx="9" cy="7" r="5" fill="url(#logo-g)" />
    <circle cx="23" cy="7" r="5" fill="url(#logo-g)" />
    <circle cx="16" cy="26" r="5" fill="url(#logo-g)" />
    <path
      d="M16 21 L16 18 Q16 12 9 12"
      stroke="url(#logo-g)" strokeWidth="3.5" strokeLinecap="round" fill="none"
    />
    <path
      d="M16 18 Q16 12 23 12"
      stroke="url(#logo-g)" strokeWidth="3.5" strokeLinecap="round" fill="none"
    />
  </svg>
);

interface LogoMarkProps {
  size?: number;
  className?: string;
}

export function LogoMark({ size = 28, className }: LogoMarkProps) {
  const [imgFailed, setImgFailed] = useState(false);

  if (imgFailed) {
    return <FallbackSvg size={size} />;
  }

  return (
    <img
      src="/logo.png"
      alt="github.mcp"
      width={size}
      height={size}
      className={className}
      style={{ objectFit: 'contain', display: 'block' }}
      onError={() => setImgFailed(true)}
    />
  );
}
