import i18n from 'i18next';
import { initReactI18next } from 'react-i18next';
import en from './en.json';
import it from './it.json';

export const languages = ['en', 'it'] as const;
export type Language = (typeof languages)[number];

const stored = typeof localStorage !== 'undefined' ? localStorage.getItem('lang') : null;
const initial: Language = languages.includes(stored as Language)
  ? (stored as Language)
  : navigator.language.toLowerCase().startsWith('it')
    ? 'it'
    : 'en'; // English is the default

document.documentElement.lang = initial;

void i18n.use(initReactI18next).init({
  resources: { en: { translation: en }, it: { translation: it } },
  lng: initial,
  fallbackLng: 'en',
  interpolation: { escapeValue: false },
});

i18n.on('languageChanged', (lng) => {
  document.documentElement.lang = lng;
  localStorage.setItem('lang', lng);
});

export default i18n;
