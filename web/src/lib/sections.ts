import {
  Activity,
  Bell,
  CreditCard,
  Database,
  KeyRound,
  LayoutDashboard,
  Plug,
  ScrollText,
  Search,
  Settings,
  Users,
  type LucideIcon,
} from 'lucide-react';

export type Section = { key: string; icon: LucideIcon; group: 0 | 1; parent?: string };

/** Order of the project column in the approved navigation. "explorer" is the project's index route. */
export const SECTIONS: Section[] = [
  { key: 'explorer', icon: Search, group: 0 },
  { key: 'live', icon: Activity, group: 0 },
  { key: 'dashboards', icon: LayoutDashboard, group: 0 },
  { key: 'alerts', icon: Bell, group: 0 },
  { key: 'integrations', icon: Plug, group: 1 },
  { key: 'storage', icon: Database, group: 1 },
  { key: 'usage', icon: CreditCard, group: 1 },
  { key: 'audit', icon: ScrollText, group: 1 },
  { key: 'settings', icon: Settings, group: 1 },
  { key: 'tokens', icon: KeyRound, group: 1, parent: 'settings' },
  { key: 'users', icon: Users, group: 1, parent: 'settings' },
];

export const sectionPath = (projectId: string, key: string) =>
  key === 'explorer' ? `/projects/${projectId}` : `/projects/${projectId}/${key}`;
