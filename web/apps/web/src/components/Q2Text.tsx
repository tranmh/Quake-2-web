'use client';
// Text drawn with the game's own bitmap font (pics/conchars.pcx: 16×16 grid of 8×8 glyphs, the high
// bit selects the alternate "green" set) from the demo pak's PNG rendition. Falls back to CSS text.
import type { CSSProperties } from 'react';
import { picUrl } from '@/lib/assets';
import { useAssetIndex } from '@/lib/useAssetIndex';
import styles from './Q2Text.module.css';

export function Q2Text({
  text,
  scale = 2,
  alt = false,
  className,
}: {
  text: string;
  scale?: number;
  /** alternate (highlighted) character set, like Com_Printf's high bit */
  alt?: boolean;
  className?: string;
}) {
  const index = useAssetIndex();
  const url = picUrl(index, 'conchars');
  if (!url) {
    return (
      <span
        className={`${styles.fallback} ${alt ? styles.alt : ''} ${className ?? ''}`}
        style={{ fontSize: 8 * scale }}
      >
        {text}
      </span>
    );
  }
  const g = 8 * scale;
  const chars = Array.from(text);
  return (
    <span className={`${styles.line} ${className ?? ''}`} role="img" aria-label={text}>
      {chars.map((ch, i) => {
        const code = (ch.charCodeAt(0) & 127) | (alt ? 128 : 0);
        const style: CSSProperties = {
          width: g,
          height: g,
          backgroundImage: `url(${url})`,
          backgroundSize: `${128 * scale}px ${128 * scale}px`,
          backgroundPosition: `-${(code & 15) * g}px -${(code >> 4) * g}px`,
        };
        return <span key={i} className={styles.glyph} style={style} aria-hidden="true" />;
      })}
    </span>
  );
}

/** A menu picture (pics/<name>.pcx rendition), e.g. the m_banner_* headings of menu.c. */
export function Q2Pic({
  name,
  fallback,
  scale = 2,
  className,
}: {
  name: string;
  fallback?: string;
  scale?: number;
  className?: string;
}) {
  const index = useAssetIndex();
  const url = picUrl(index, name);
  const e = index?.files[`pics/${name.toLowerCase()}.pcx`];
  if (!url || !e?.png)
    return fallback ? <Q2Text text={fallback} scale={scale} className={className} /> : null;
  return (
    <img
      src={url}
      alt={fallback ?? name}
      width={e.png.width * scale}
      height={e.png.height * scale}
      className={`${styles.pic} ${className ?? ''}`}
    />
  );
}
