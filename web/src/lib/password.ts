export function validPassword(password: string, identity: { email?: string; username?: string } = {}): boolean {
  return [...password].length >= 10 && new TextEncoder().encode(password).length <= 128
    && ![identity.email?.toLowerCase(), identity.username?.toLowerCase()].filter(Boolean).includes(password.toLowerCase());
}
