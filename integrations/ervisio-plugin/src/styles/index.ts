/**
 * All CSS of the plugin as one <style>, injected once. Every .css file in this folder is included in alphabetical
 * order. Theme variables only; class names start with `el-`.
 */
const files = import.meta.glob<string>('./*.css', { query: '?inline', import: 'default', eager: true });

export function injectStyles(): void {
  if (document.getElementById('el-styles')) return;
  const el = document.createElement('style');
  el.id = 'el-styles';
  el.textContent = Object.keys(files).sort().map((k) => files[k]).join('\n');
  document.head.appendChild(el);
}
