import { Route, Routes } from 'react-router-dom';
import Shell from './components/Shell';
import Overview from './pages/Overview';

export default function App() {
  return (
    <Routes>
      <Route element={<Shell />}>
        <Route index element={<Overview />} />
        <Route path="*" element={<Overview />} />
      </Route>
    </Routes>
  );
}
