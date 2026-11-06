import { Bell, KeyRound, LayoutDashboard, Search, Settings, type LucideIcon } from 'lucide-react';

export type Section = { key: string; icon: LucideIcon; group: 0 | 1; parent?: string; adminOnly?: boolean };
export const SECTIONS: Section[] = [
  { key: 'explorer', icon: Search, group: 0 },
  { key: 'alerts', icon: Bell, group: 0 },
  { key: 'dashboards', icon: LayoutDashboard, group: 0 },
  { key: 'settings', icon: Settings, group: 1 },
  { key: 'tokens', icon: KeyRound, group: 1, parent: 'settings' },
];
export const sectionPath = (projectId: string, key: string, workspaceId?: string) => {
  const base = workspaceId ? `/w/${workspaceId}/p/${projectId}` : `/projects/${projectId}`;
  return key === 'explorer' ? base : `${base}/${key}`;
};
