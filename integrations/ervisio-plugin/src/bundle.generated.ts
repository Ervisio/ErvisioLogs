// Written by scripts/release.sh with the release's compose.yaml and .env.example. A development checkout carries no
// release and cannot install anything.
export const BUNDLE: { version: string; compose: string | null; envExample: string | null } = {
  version: '0.0.0-dev',
  compose: null,
  envExample: null,
};
