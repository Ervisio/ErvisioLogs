import { Outlet, Route, Routes } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import Shell from './components/Shell';
import { AuthProvider, RequireAuth, useAuth } from './lib/auth';
import { TenancyProvider } from './lib/tenancy';
import Login from './pages/Login';
import Overview from './pages/Overview';
import Documentation from './pages/Documentation';
import { Explorer, Home, NotFound, Projects, SectionPage } from './pages/Workspace';

function FullPageLoading() {
  const { t } = useTranslation();
  return (
    <main className="text-ink2 flex min-h-dvh items-center justify-center gap-2" role="status">
      <span className="spinner" aria-hidden />
      {t('login.loading')}
    </main>
  );
}

function FullPageUnreachable() {
  const { t } = useTranslation();
  const { retry } = useAuth();
  return (
    <main className="flex min-h-dvh items-center justify-center p-6">
      <div role="alert" className="max-w-[360px] text-center">
        <h1 className="mb-1 text-[17px] font-bold">{t('login.unreachableTitle')}</h1>
        <p className="text-ink2">{t('login.unreachableText')}</p>
        <button type="button" onClick={retry} className="text-hue mt-2 min-h-11 font-semibold underline underline-offset-2">
          {t('common.retry')}
        </button>
      </div>
    </main>
  );
}

function Workspace() {
  return (
    <TenancyProvider>
      <Outlet />
    </TenancyProvider>
  );
}

export default function App() {
  return (
    <AuthProvider>
      <Routes>
        <Route path="login" element={<Login />} />
        <Route element={<RequireAuth loading={<FullPageLoading />} unreachable={<FullPageUnreachable />} />}>
          <Route element={<Workspace />}>
            <Route element={<Shell />}>
              <Route index element={<Home />} />
              <Route path="status" element={<Overview />} />
              <Route path="docs" element={<Documentation />} />
              <Route path="docs/:topic" element={<Documentation />} />
              <Route path="projects" element={<Projects />} />
              <Route path="projects/:projectId" element={<Explorer />} />
              <Route path="projects/:projectId/streams/:streamId" element={<Explorer />} />
              <Route path="projects/:projectId/:section" element={<SectionPage />} />
              <Route path="*" element={<NotFound />} />
            </Route>
          </Route>
        </Route>
      </Routes>
    </AuthProvider>
  );
}
