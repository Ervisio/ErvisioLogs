/** Lowercase ASCII words joined by dashes. Accents fold to their base letter; scripts with no Latin form drop out. */
export function slugify(name: string, max = 39): string {
  return name
    .normalize('NFD')
    .replace(/[̀-ͯ]/g, '')
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, max)
    .replace(/-+$/, '');
}

/** A slug the server accepts even when the name has no ASCII letters, where it would derive an empty one and refuse. */
export function slugFor(name: string, fallback: string): string {
  return slugify(name, 30) || `${fallback}-${Date.now().toString(36)}`;
}
