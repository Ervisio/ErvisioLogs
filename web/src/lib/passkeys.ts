import { request, type Session } from './api';

type Begin = { challenge: string; options: { publicKey: PublicKeyCredentialCreationOptionsJSON } };
type LoginBegin = { challenge: string; options: { publicKey: PublicKeyCredentialRequestOptionsJSON } };
export function supportsPasskeys() {
  return (
    typeof PublicKeyCredential !== 'undefined' &&
    typeof PublicKeyCredential.parseCreationOptionsFromJSON === 'function' &&
    typeof PublicKeyCredential.parseRequestOptionsFromJSON === 'function' &&
    typeof PublicKeyCredential.prototype.toJSON === 'function'
  );
}
export async function registerPasskey(password: string, code: string, name: string) {
  const start = await request<Begin>('POST', '/auth/passkeys/register/start', { password, code, name });
  const credential = await navigator.credentials.create({
    publicKey: PublicKeyCredential.parseCreationOptionsFromJSON(start.options.publicKey),
  });
  if (!(credential instanceof PublicKeyCredential)) throw new Error('Passkey creation cancelled');
  await request('POST', '/auth/passkeys/register/finish', {
    challenge: start.challenge,
    credential: credential.toJSON(),
  });
}
export async function loginWithPasskey(login: string) {
  const start = await request<LoginBegin>('POST', '/auth/passkeys/login/start', { login });
  const credential = await navigator.credentials.get({
    publicKey: PublicKeyCredential.parseRequestOptionsFromJSON(start.options.publicKey),
  });
  if (!(credential instanceof PublicKeyCredential)) throw new Error('Passkey sign-in cancelled');
  return request<Session>('POST', '/auth/passkeys/login/finish', {
    challenge: start.challenge,
    credential: credential.toJSON(),
  });
}
