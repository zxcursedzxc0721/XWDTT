import { useState } from 'react';
import { useNavigate, useLocation } from 'react-router-dom';
import {
  IconPlugConnected,
  IconTerminal2,
  IconSettings2,
  IconSun,
  IconMoon,
} from '@tabler/icons-react';
import { themeStore } from '../lib/stores/themeStore';

const NAV = [
  { path: '/', icon: <IconPlugConnected stroke={2} size={22} /> },
  { path: '/logs', icon: <IconTerminal2 stroke={2} size={22} /> },
];

const handleMouseMove = (e: React.MouseEvent<HTMLElement>) => {
  const el = e.currentTarget;
  const rect = el.getBoundingClientRect();
  const x = ((e.clientX - rect.left) / rect.width) * 100;
  const y = ((e.clientY - rect.top) / rect.height) * 100;
  el.style.setProperty('--mx', `${x}%`);
  el.style.setProperty('--my', `${y}%`);
};

interface Props {
  onSettings?: () => void;
  pathname?: string;
}

export default function Sidebar({ onSettings, pathname: pathnameProp }: Props) {
  const navigate = useNavigate();
  const location = useLocation();
  const pathname = pathnameProp ?? location.pathname;
  const [theme, setTheme] = useState(() => themeStore.get());

  const toggleTheme = () => {
    themeStore.toggle();
    setTheme(themeStore.get());
  };

  return (
    <>
      <style>{`
        .sidebar { width: 70px; background: linear-gradient(160deg, var(--sidebar-from), var(--sidebar-to)); border-radius: 12px; margin: 2px; display: flex; flex-direction: column; justify-content: space-between; padding: 16px 0; overflow: hidden; flex-shrink: 0; position: relative; }
        .sidebar::before { content: ''; position: absolute; inset: 0; border-radius: inherit; pointer-events: none; mix-blend-mode: overlay; opacity: 0.35; background-image: url("data:image/svg+xml,%3Csvg viewBox='0 0 512 512' xmlns='http://www.w3.org/2000/svg'%3E%3Cfilter id='n'%3E%3CfeTurbulence type='fractalNoise' baseFrequency='0.75' numOctaves='4' stitchTiles='stitch'/%3E%3C/filter%3E%3Crect width='100%25' height='100%25' filter='url(%23n)'/%3E%3C/svg%3E"); background-size: 200px 200px; }
        .sidebar::after { content: ''; position: absolute; inset: 0; border-radius: inherit; pointer-events: none; opacity: 0; background: radial-gradient(circle at var(--mx, 50%) var(--my, 50%), rgba(255,255,255,0.15) 0%, transparent 50%); transition: opacity 0.2s; }
        .sidebar:hover::after { opacity: 1; }
        .sidebar-top, .sidebar-bottom { display: flex; flex-direction: column; align-items: center; gap: 8px; position: relative; z-index: 1; }
        .nav-btn { width: 48px; height: 48px; border: none; border-radius: 12px; background: transparent; color: var(--sidebar-text, #fff); cursor: pointer; display: flex; align-items: center; justify-content: center; opacity: 0.75; transition: opacity 0.2s, background 0.3s ease, border-radius 0.3s ease, box-shadow 0.15s, transform 0.2s; position: relative; }
        .nav-btn:hover { opacity: 1; transform: scale(1.08); }
        .nav-btn--active { background: var(--sidebar-btn-active); opacity: 1; border-radius: 16px 16px 16px 2px; box-shadow: 0 2px 8px rgba(0,0,0,0.3); }
        .theme-toggle { background: none; border: none; cursor: pointer; padding: 4px; display: flex; align-items: center; justify-content: center; color: var(--sidebar-text, #fff); opacity: 0.7; border-radius: 8px; transition: opacity 0.15s, transform 0.2s; }
        .theme-toggle:hover { opacity: 1; transform: rotate(20deg); }
        .theme-toggle svg { transition: transform 0.35s ease, opacity 0.35s ease; }
        .theme-toggle:active svg { transform: rotate(180deg) scale(0.8); opacity: 0.4; }
        @keyframes icon-swap { 0% { transform: rotate(-90deg) scale(0.5); opacity: 0; } 100% { transform: rotate(0deg) scale(1); opacity: 1; } }
        .theme-toggle svg { animation: icon-swap 0.3s ease-out; }
      `}</style>
      <aside className="sidebar" onMouseMove={handleMouseMove}>
        <div className="sidebar-top">
          {NAV.map(({ path, icon }) => (
            <button
              type="button"
              key={path}
              className={`nav-btn${pathname === path ? ' nav-btn--active' : ''}`}
              onClick={() => navigate(path)}
            >
              {icon}
            </button>
          ))}
        </div>
        <div className="sidebar-bottom">
          <button type="button" className="theme-toggle" onClick={toggleTheme} title="Toggle theme" aria-label="Переключить тему">
            {theme === 'light' ? <IconMoon size={17} stroke={2} /> : <IconSun size={17} stroke={2} />}
          </button>
          <button type="button" className="nav-btn" onClick={onSettings} aria-label="Настройки">
            <IconSettings2 stroke={2} size={22} />
          </button>
        </div>
      </aside>
    </>
  );
}
