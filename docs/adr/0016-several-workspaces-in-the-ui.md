# ADR 0016: Create and switch workspaces from the projects page

Status: Proposed. Written in 0.1.3. Checked with `tsc` and `vite build` only.

## Context
The API and schema already allow many workspaces per user (`POST /workspaces`, `workspace_members`), each with its
own projects, roles, tokens and channels. The web UI offered the "create workspace" form only while the user had
none, so the owner could make one workspace and no more. Switching sat in the account menu and appeared only with
two or more workspaces.

## Options
1. A separate workspaces page with its own route.
2. Put the workspaces on the projects page: the list of workspaces first, a "New workspace" form, then the projects
   of the selected one.

## Decision
Option 2. `/projects` becomes "Workspaces and projects". Picking a workspace selects it (same store as the account
menu switcher), and new projects go into the selected one. A new workspace makes the creator its owner and becomes
the selected one. No backend change.

## Risks
- The rail and the explorer show the projects of the selected workspace only; someone with many workspaces switches
  on this page or in the account menu.
- Rename and delete of a workspace still have no UI (the API has them).

## Tests needed
Create a second workspace, add a project to each, switch between them and check the rail, tokens and members follow.
Not run.
