import { Bell, Database, KeyRound, Plug, Search, Users, type LucideIcon } from 'lucide-react';

export type Section = { key: string; icon: LucideIcon; group: 0 | 1; adminOnly?: boolean };

export const SECTIONS: Section[] = [
  { key: 'explorer', icon: Search, group: 0 },
  { key: 'alerts', icon: Bell, group: 0 },
  { key: 'tokens', icon: KeyRound, group: 1 },
  { key: 'integrations', icon: Plug, group: 1 },
  { key: 'users', icon: Users, group: 1 },
  { key: 'storage', icon: Database, group: 1, adminOnly: true },
];

export const workspacePath = (workspaceId: string, suffix = 'projects') =>
  `/w/${encodeURIComponent(workspaceId)}${suffix ? `/${suffix}` : ''}`;

export const projectPath = (workspaceId: string, projectId: string, suffix = '') =>
  `${workspacePath(workspaceId, '')}/p/${encodeURIComponent(projectId)}${suffix ? `/${suffix}` : ''}`;

// Older links in page content remain valid through the legacy project redirect.
export const sectionPath = (projectId: string, key: string) =>
  key === 'explorer' ? `/projects/${projectId}` : `/projects/${projectId}/${key}`;
