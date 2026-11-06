export const projectPath = (workspaceId: string, projectId: string, section = '') =>
  `/w/${encodeURIComponent(workspaceId)}/p/${encodeURIComponent(projectId)}${section ? `/${section}` : ''}`;
export const workspacePath = (workspaceId: string, section = 'general') =>
  `/w/${encodeURIComponent(workspaceId)}/settings/${section}`;
