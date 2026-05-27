import { useState, useEffect } from 'react';
import { authService } from './authService';
import {
  Activity,
  Folder,
  Key,
  LogOut
} from 'lucide-react';
import Avatar from './components/Avatar';
import Login from './pages/Login';
import Dashboard from './pages/Dashboard';
import Repository from './pages/Repository';
import CommitDetail from './pages/CommitDetail';
import Tokens from './pages/Tokens';
import Security from './pages/Security';
import BuildLogs from './pages/BuildLogs';
import SettingsAppsNew from './pages/SettingsAppsNew';
import SettingsAppsInstall from './pages/SettingsAppsInstall';
import SettingsApps from './pages/SettingsApps';
import Profile from './pages/Profile';

const parseLocation = () => {
  const path = window.location.pathname;
  const searchParams = new URLSearchParams(window.location.search);
  const segments = path.split('/').filter(Boolean);

  // 1. Dashboard
  if (segments.length === 0) {
    return { page: 'dashboard', params: {} };
  }

  // 2. Access Tokens
  if (segments.length === 1 && segments[0] === 'tokens') {
    return { page: 'tokens', params: {} };
  }

  // 2b. Security
  if (segments.length === 1 && segments[0] === 'security') {
    return { page: 'security', params: {} };
  }

  // 3. Pull Requests List
  // Pattern: /:owner/:repo/pulls
  if (segments.length === 3 && segments[2] === 'pulls') {
    return { 
      page: 'pulls', 
      params: { 
        owner: segments[0], 
        repo: segments[1] 
      } 
    };
  }

  // 4. Pull Request Detail or New Pull Request
  // Pattern: /:owner/:repo/pulls/:number or /:owner/:repo/pulls/new
  if (segments.length === 4 && segments[2] === 'pulls') {
    if (segments[3] === 'new') {
      return {
        page: 'pull_new',
        params: {
          owner: segments[0],
          repo: segments[1]
        }
      };
    }
    return { 
      page: 'pull_detail', 
      params: { 
        owner: segments[0], 
        repo: segments[1], 
        number: segments[3] 
      } 
    };
  }

  // 5. Build Logs Console
  // Pattern: /:owner/:repo/commit/:sha/builds/:buildId
  if (segments.length === 6 && segments[2] === 'commit' && segments[4] === 'builds') {
    return {
      page: 'build_logs',
      params: {
        owner: segments[0],
        repo: segments[1],
        sha: segments[3],
        buildId: segments[5]
      }
    };
  }

  // 6. Commit Detail
  // Pattern: /:owner/:repo/commit/:sha
  if (segments.length === 4 && segments[2] === 'commit') {
    return {
      page: 'commit',
      params: {
        owner: segments[0],
        repo: segments[1],
        sha: segments[3]
      }
    };
  }

  // Plan 4 Task 7: GitHub Apps list page.
  // Pattern: /settings/apps
  // Must appear BEFORE the 2-segment /:owner/:repo branch.
  if (segments.length === 2 && segments[0] === 'settings' && segments[1] === 'apps') {
    return { page: 'apps_list', params: {} };
  }

  // 5b. User Profile (new)
  // Pattern: /u/:username — MUST appear BEFORE the 2-segment /:owner/:repo branch.
  // Safe to reserve "u": username regex ^[a-zA-Z0-9-]{3,20}$ (min 3 chars) means a
  // single-char first segment can never be a real owner.
  if (segments.length === 2 && segments[0] === 'u') {
    return { page: 'profile', params: { username: segments[1] } };
  }

  // 6. Repository Details & Tabs
  // Pattern: /:owner/:repo
  if (segments.length === 2) {
    return {
      page: 'repository',
      params: {
        owner: segments[0],
        repo: segments[1],
        tab: searchParams.get('tab') || 'code',
        path: searchParams.get('path') || ''
      }
    };
  }

  // Plan 4: GitHub App manifest confirmation page.
  // Pattern: /settings/apps/new (with ?manifest=<URL-encoded JSON>)
  if (segments.length === 3 && segments[0] === 'settings' && segments[1] === 'apps' && segments[2] === 'new') {
    return { page: 'apps_new', params: {} };
  }

  // Plan 4 Task 6: App install page.
  // Pattern: /settings/apps/{slug}/installations/new
  if (segments.length === 5 && segments[0] === 'settings' && segments[1] === 'apps'
      && segments[3] === 'installations' && segments[4] === 'new') {
    return { page: 'apps_install', params: { slug: segments[2] } };
  }

  // Fallback
  return { page: 'dashboard', params: {} };
};

export default function App() {
  const [loading, setLoading] = useState(true);
  const [user, setUser] = useState(null);
  
  // Custom SPA Router state synchronized with browser URL
  const [navigation, setNavigation] = useState(() => parseLocation());

  // Listen to browser history navigation (back/forward buttons)
  useEffect(() => {
    const handlePopState = () => {
      setNavigation(parseLocation());
    };

    window.addEventListener('popstate', handlePopState);
    return () => window.removeEventListener('popstate', handlePopState);
  }, []);

  useEffect(() => {
    // Initialize Auth
    const initAuth = async () => {
      await authService.init();
      setLoading(false);
    };
    
    initAuth();

    // Subscribe to auth changes
    const unsubscribe = authService.onAuthStateChanged((currentUser) => {
      setUser(currentUser);
    });

    return () => unsubscribe();
  }, []);

  // Synchronize state and URL smoothly without reload
  const navigate = (page, params = {}, replace = false) => {
    let url = '/';
    if (page === 'dashboard') {
      url = '/';
    } else if (page === 'tokens') {
      url = '/tokens';
    } else if (page === 'security') {
      url = '/security';
    } else if (page === 'repository') {
      const { owner, repo, tab, path } = params;
      url = `/${owner}/${repo}`;
      const queryParts = [];
      if (tab && tab !== 'code') {
        queryParts.push(`tab=${tab}`);
      }
      if (path) {
        queryParts.push(`path=${encodeURIComponent(path)}`);
      }
      if (queryParts.length > 0) {
        url += `?${queryParts.join('&')}`;
      }
    } else if (page === 'commit') {
      const { owner, repo, sha } = params;
      url = `/${owner}/${repo}/commit/${sha}`;
    } else if (page === 'build_logs') {
      const { owner, repo, sha, buildId } = params;
      url = `/${owner}/${repo}/commit/${sha}/builds/${buildId}`;
    } else if (page === 'pulls') {
      const { owner, repo } = params;
      url = `/${owner}/${repo}/pulls`;
    } else if (page === 'pull_new') {
      const { owner, repo } = params;
      url = `/${owner}/${repo}/pulls/new`;
    } else if (page === 'pull_detail') {
      const { owner, repo, number } = params;
      url = `/${owner}/${repo}/pulls/${number}`;
    } else if (page === 'apps_install') {
      const { slug } = params;
      url = `/settings/apps/${slug}/installations/new`;
    } else if (page === 'apps_list') {
      url = '/settings/apps';
    } else if (page === 'profile') {
      url = `/u/${params.username}`;
    }

    if (replace) {
      window.history.replaceState(null, '', url);
    } else {
      window.history.pushState(null, '', url);
    }
    setNavigation({ page, params });
    // Scroll to top
    window.scrollTo(0, 0);
  };

  const handleLogout = async () => {
    await authService.logout();
    navigate('dashboard', {}, true);
  };

  if (loading) {
    return (
      <div style={{
        display: 'flex', 
        flexDirection: 'column',
        alignItems: 'center', 
        justifyContent: 'center', 
        height: '100vh',
        background: '#06080d',
        gap: '1rem'
      }}>
        <div className="loader" style={{ width: '40px', height: '40px', borderWidth: '4px' }}></div>
        <div style={{ color: '#94a3b8', fontSize: '1rem', fontWeight: 500 }}>Initializing GitBucket...</div>
      </div>
    );
  }

  // Render correct view based on navigation state
  const renderView = () => {
    if (!user) return <Login onNavigate={navigate} currentNavigation={navigation} />;

    switch (navigation.page) {
      case 'dashboard':
        return <Dashboard user={user} onNavigate={navigate} />;
      case 'repository':
        return (
          <Repository 
            user={user} 
            owner={navigation.params.owner} 
            repo={navigation.params.repo} 
            initialTab={navigation.params.tab || 'code'}
            initialPath={navigation.params.path || ''}
            onNavigate={navigate} 
          />
        );
      case 'commit':
        return (
          <CommitDetail 
            user={user} 
            owner={navigation.params.owner} 
            repo={navigation.params.repo} 
            sha={navigation.params.sha} 
            onNavigate={navigate} 
          />
        );
      case 'build_logs':
        return (
          <BuildLogs
            user={user}
            owner={navigation.params.owner}
            repo={navigation.params.repo}
            sha={navigation.params.sha}
            buildId={navigation.params.buildId}
            onNavigate={navigate}
          />
        );
      case 'tokens':
        return <Tokens user={user} onNavigate={navigate} />;
      case 'security':
        return <Security user={user} onNavigate={navigate} />;
      case 'apps_new':
        return <SettingsAppsNew user={user} onNavigate={navigate} />;
      case 'apps_install':
        return <SettingsAppsInstall slug={navigation.params.slug} onNavigate={navigate} />;
      case 'apps_list':
        return <SettingsApps onNavigate={navigate} />;
      case 'profile':
        return (
          <Profile user={user} username={navigation.params.username} onNavigate={navigate} />
        );
      case 'pulls':
        return (
          <Repository 
            user={user} 
            owner={navigation.params.owner} 
            repo={navigation.params.repo} 
            initialTab="pulls"
            onNavigate={navigate} 
          />
        );
      case 'pull_detail':
        return (
          <Repository 
            user={user} 
            owner={navigation.params.owner} 
            repo={navigation.params.repo} 
            initialTab="pull_detail"
            prNumber={navigation.params.number}
            onNavigate={navigate} 
          />
        );
      case 'pull_new':
        return (
          <Repository 
            user={user} 
            owner={navigation.params.owner} 
            repo={navigation.params.repo} 
            initialTab="pull_new"
            onNavigate={navigate} 
          />
        );
      default:
        return <Dashboard user={user} onNavigate={navigate} />;
    }
  };

  return (
    <div className="app-container">
      {user && (
        <header className="gb-header">
          <div className="logo" onClick={() => navigate('dashboard')}>
            <span className="mark">G</span>
            <span>GitBucket</span>
          </div>

          <nav className="nav">
            <button
              className={navigation.page === 'dashboard' ? 'active' : ''}
              onClick={() => navigate('dashboard')}
            >
              <Folder size={13} />
              Repositories
            </button>
            {/* Activity has no route this pass — rendered to match the design, inert. */}
            <button type="button" aria-disabled="true">
              <Activity size={13} />
              Activity
            </button>
            <button
              className={navigation.page === 'tokens' ? 'active' : ''}
              onClick={() => navigate('tokens')}
            >
              <Key size={13} />
              Access tokens
            </button>
          </nav>

          <div className="user-block">
            <Avatar name={user.username || 'user'} size={32} />
            <div className="user-meta">
              <span className="user-name">@{user.username || 'user'}</span>
              <span className="user-email">{user.email}</span>
            </div>
            <button className="logout" onClick={handleLogout} title="Sign out">
              <LogOut size={14} />
            </button>
          </div>
        </header>
      )}

      <main className="main-content">
        {renderView()}
      </main>
      
      <footer style={{
        textAlign: 'center', 
        padding: '2rem', 
        color: '#64748b', 
        fontSize: '0.85rem',
        borderTop: '1px solid rgba(255, 255, 255, 0.05)',
        marginTop: 'auto'
      }}>
        GitBucket &copy; 2026 &bull; Cloud Run &amp; Google Cloud Storage Git Hosting
      </footer>
    </div>
  );
}
