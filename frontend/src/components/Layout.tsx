import { useState } from 'react';
import { Outlet, useLocation } from 'react-router-dom';
import Sidebar from './Sidebar';
import Settings from '../modals/Settings';

export default function Layout() {
  const [settingsOpen, setSettingsOpen] = useState(false);
  const { pathname } = useLocation();

  return (
    <div style={{ display: 'flex', height: '100vh', background: 'var(--surface)', boxSizing: 'border-box' }}>
      <Sidebar
        onSettings={() => setSettingsOpen(true)}
        pathname={pathname}
      />
      <div style={{ flex: 1, display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
        <Outlet />
      </div>
      {settingsOpen && <Settings onClose={() => setSettingsOpen(false)} />}
    </div>
  );
}
