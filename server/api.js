import { Router } from 'express';
import { spawnSync } from 'child_process';
import path from 'path';
import fs from 'fs';
import admin from 'firebase-admin';
import { 
  db,
  getRepositoryMetadata,
  createRepositoryMetadata,
  updateRepositoryMetadata,
  deleteRepositoryMetadata,
  listUserRepositories,
  listAllPublicRepositories,
  registerUsername,
  getUsernameByUid,
  generatePAT,
  listPATs,
  revokePAT,
  acquireLock,
  releaseLock
} from './db.js';
import { downloadRepo, deleteRepo, LOCAL_REPOS_ROOT } from './gcs.js';
import { getSyncFilePath, writeLocalSyncTimestamp, isLocalCacheUpToDate } from './utils.js';

const router = Router();

// ----------------------------------------------------
// AUTH MIDDLEWARES
// ----------------------------------------------------

export async function requireWebAuth(req, res, next) {
  const authHeader = req.headers.authorization;
  if (!authHeader || !authHeader.startsWith('Bearer ')) {
    return res.status(401).json({ error: 'Unauthorized. Token required.' });
  }
  const token = authHeader.substring(7);
  try {
    let decodedToken;
    if (process.env.DEV_MODE === 'true' && token.startsWith('mock_')) {
      const mockUid = token.split('_')[1] || 'devuser';
      decodedToken = { uid: mockUid, email: `${mockUid}@example.com` };
    } else {
      decodedToken = await admin.auth().verifyIdToken(token);
    }
    req.user = decodedToken;
    req.user.username = await getUsernameByUid(decodedToken.uid);
    next();
  } catch (err) {
    console.error('Error verifying Firebase ID token:', err);
    return res.status(401).json({ error: 'Unauthorized. Invalid token.' });
  }
}

export async function optionalWebAuth(req, res, next) {
  const authHeader = req.headers.authorization;
  if (authHeader && authHeader.startsWith('Bearer ')) {
    const token = authHeader.substring(7);
    try {
      let decodedToken;
      if (process.env.DEV_MODE === 'true' && token.startsWith('mock_')) {
        const mockUid = token.split('_')[1] || 'devuser';
        decodedToken = { uid: mockUid, email: `${mockUid}@example.com` };
      } else {
        decodedToken = await admin.auth().verifyIdToken(token);
      }
      req.user = decodedToken;
      req.user.username = await getUsernameByUid(decodedToken.uid);
    } catch (err) {
      // Ignore invalid/expired token for optional auth
    }
  }
  next();
}

/**
 * Ensures the local container cache is up to date before reading from it.
 */
async function syncRepoCache(owner, repo, repoMeta) {
  const needsSync = !isLocalCacheUpToDate(owner, repo, repoMeta.updatedAt);
  if (needsSync) {
    console.log(`[API Sync] Cache outdated for ${owner}/${repo}. Syncing...`);
    let lockToken = null;
    try {
      // Lock to avoid concurrent write/download issues
      lockToken = await acquireLock(owner, repo);
      // Double check check after lock acquisition
      if (!isLocalCacheUpToDate(owner, repo, repoMeta.updatedAt)) {
        await downloadRepo(owner, repo);
        writeLocalSyncTimestamp(owner, repo, repoMeta.updatedAt);
      }
    } finally {
      if (lockToken) await releaseLock(owner, repo, lockToken);
    }
  }
}

// ----------------------------------------------------
// USER ENDPOINTS
// ----------------------------------------------------

/**
 * Register username (must be unique)
 */
router.post('/user/username', requireWebAuth, async (req, res) => {
  const { username } = req.body;
  if (!username || !/^[a-zA-Z0-9-]{3,20}$/.test(username)) {
    return res.status(400).json({ error: 'Username must be alphanumeric, 3-20 characters, optionally containing hyphens.' });
  }

  try {
    await registerUsername(req.user.uid, username, req.user.email);
    res.json({ success: true, username: username.toLowerCase() });
  } catch (err) {
    res.status(400).json({ error: err.message });
  }
});

/**
 * Get current authenticated user details
 */
router.get('/user/me', requireWebAuth, async (req, res) => {
  res.json({
    uid: req.user.uid,
    email: req.user.email,
    username: req.user.username || null
  });
});

// ----------------------------------------------------
// PERSONAL ACCESS TOKEN (PAT) ENDPOINTS
// ----------------------------------------------------

router.get('/tokens', requireWebAuth, async (req, res) => {
  try {
    const tokens = await listPATs(req.user.uid);
    res.json(tokens);
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

router.post('/tokens', requireWebAuth, async (req, res) => {
  const { name } = req.body;
  if (!name || name.trim().length === 0) {
    return res.status(400).json({ error: 'Token label/name is required.' });
  }

  try {
    const token = await generatePAT(req.user.uid, name.trim());
    res.json({ token, name });
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

router.delete('/tokens/:tokenId', requireWebAuth, async (req, res) => {
  const { tokenId } = req.params;
  try {
    const success = await revokePAT(req.user.uid, tokenId);
    if (success) {
      res.json({ success: true });
    } else {
      res.status(404).json({ error: 'Token not found or unauthorized' });
    }
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

// ----------------------------------------------------
// REPOSITORY ENDPOINTS
// ----------------------------------------------------

/**
 * List repositories accessible to the user
 */
router.get('/repos', optionalWebAuth, async (req, res) => {
  try {
    const publicRepos = await listAllPublicRepositories();
    if (!req.user || !req.user.username) {
      return res.json(publicRepos);
    }

    // Authenticated user: merge their own repositories (even private ones)
    const userRepos = await listUserRepositories(req.user.username);
    
    // De-duplicate by ID
    const allReposMap = new Map();
    publicRepos.forEach(r => allReposMap.set(`${r.owner}/${r.name}`, r));
    userRepos.forEach(r => allReposMap.set(`${r.owner}/${r.name}`, r));

    res.json(Array.from(allReposMap.values()));
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

/**
 * Create a new repository
 */
router.post('/repos', requireWebAuth, async (req, res) => {
  const { name, description, visibility } = req.body;
  const username = req.user.username;

  if (!username) {
    return res.status(403).json({ error: 'You must set a username before creating a repository.' });
  }

  if (!name || !/^[a-zA-Z0-9-_]{3,30}$/.test(name)) {
    return res.status(400).json({ error: 'Repository name must be 3-30 characters, alphanumeric, hyphens, or underscores.' });
  }

  const visibilityStr = visibility === 'private' ? 'private' : 'public';

  try {
    await createRepositoryMetadata(req.user.uid, username, name, description, visibilityStr);
    res.json({ success: true, owner: username, name });
  } catch (err) {
    res.status(400).json({ error: err.message });
  }
});

/**
 * Get single repository details
 */
router.get('/repos/:owner/:repo', optionalWebAuth, async (req, res) => {
  const { owner, repo } = req.params;

  try {
    const meta = await getRepositoryMetadata(owner, repo);
    if (!meta) {
      return res.status(404).json({ error: 'Repository not found' });
    }

    // Check visibility
    const isOwner = req.user && req.user.username && req.user.username.toLowerCase() === owner.toLowerCase();
    if (meta.visibility === 'private' && !isOwner) {
      return res.status(403).json({ error: 'Access denied. Private repository.' });
    }

    res.json(meta);
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

/**
 * Delete repository
 */
router.delete('/repos/:owner/:repo', requireWebAuth, async (req, res) => {
  const { owner, repo } = req.params;
  const username = req.user.username;

  if (!username || username.toLowerCase() !== owner.toLowerCase()) {
    return res.status(403).json({ error: 'Only the repository owner can delete this repository.' });
  }

  try {
    // Delete GCS storage
    await deleteRepo(owner, repo);
    // Delete Firestore metadata
    await deleteRepositoryMetadata(owner, repo);
    
    res.json({ success: true });
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

// ----------------------------------------------------
// GIT BROWSER / UI API ENDPOINTS
// ----------------------------------------------------

// Middleware to authorize Git browser actions
async function authorizeGitRead(req, res, next) {
  const { owner, repo } = req.params;
  try {
    const meta = await getRepositoryMetadata(owner, repo);
    if (!meta) {
      return res.status(404).json({ error: 'Repository not found' });
    }

    const isOwner = req.user && req.user.username && req.user.username.toLowerCase() === owner.toLowerCase();
    if (meta.visibility === 'private' && !isOwner) {
      return res.status(403).json({ error: 'Access denied.' });
    }

    req.repoMeta = meta;
    req.localRepoPath = path.join(LOCAL_REPOS_ROOT, owner.toLowerCase(), `${repo.toLowerCase()}.git`);
    
    // Ensure cache is synced
    await syncRepoCache(owner, repo, meta);
    
    next();
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
}

/**
 * Get commit history for a branch
 */
router.get('/repos/:owner/:repo/commits/:branch', optionalWebAuth, authorizeGitRead, async (req, res) => {
  const { branch } = req.params;
  const limit = req.query.limit ? parseInt(req.query.limit, 10) : 50;

  // Run: git log -n <limit> --format="%H|%an|%ae|%ad|%s" <branch>
  const git = spawnSync('git', [
    '--git-dir', req.localRepoPath,
    'log',
    `-n`, `${limit}`,
    '--format=%H|%an|%ae|%ad|%s',
    branch
  ]);

  if (git.status !== 0) {
    const stderr = git.stderr.toString();
    // If no commits yet
    if (stderr.includes('does not have any commits') || stderr.includes('fatal: bad default revision')) {
      return res.json([]);
    }
    return res.status(500).json({ error: 'Failed to read commit logs', details: stderr });
  }

  const commits = git.stdout.toString()
    .split('\n')
    .filter(Boolean)
    .map(line => {
      const [sha, authorName, authorEmail, date, message] = line.split('|');
      return { sha, authorName, authorEmail, date, message };
    });

  res.json(commits);
});

/**
 * Get a specific commit's diff details
 */
router.get('/repos/:owner/:repo/commit/:sha', optionalWebAuth, authorizeGitRead, async (req, res) => {
  const { sha } = req.params;

  // Run: git show --patch --format=raw <sha>
  const git = spawnSync('git', [
    '--git-dir', req.localRepoPath,
    'show',
    sha
  ]);

  if (git.status !== 0) {
    return res.status(500).json({ error: 'Failed to read commit details', details: git.stderr.toString() });
  }

  res.json({ rawDiff: git.stdout.toString() });
});

/**
 * Get folder tree / file listing at a path
 */
router.get('/repos/:owner/:repo/tree/:branch/*', optionalWebAuth, authorizeGitRead, async (req, res) => {
  const { branch } = req.params;
  // Express wildcard * is in req.params[0]
  const filePath = req.params[0] || '';

  // Run: git ls-tree -l --full-name <branch>:<path>
  const refPath = filePath ? `${branch}:${filePath}` : `${branch}`;
  
  const git = spawnSync('git', [
    '--git-dir', req.localRepoPath,
    'ls-tree',
    '-l',
    '--full-name',
    refPath
  ]);

  if (git.status !== 0) {
    const stderr = git.stderr.toString();
    if (stderr.includes('fatal: Not a valid object name') || stderr.includes('fatal: path') || stderr.includes('does not exist')) {
      // Empty repository or directory not found
      return res.json([]);
    }
    return res.status(500).json({ error: 'Failed to list files', details: stderr });
  }

  const items = git.stdout.toString()
    .split('\n')
    .filter(Boolean)
    .map(line => {
      // Format: <mode> <type> <object> <size>    <file>
      // e.g.: 100644 blob 1e3c8f...      123    README.md
      // directory: 040000 tree 8e5f...       -    src
      const match = line.match(/^(\d+)\s+(\w+)\s+([0-9a-fA-F]+)\s+(\d+|-)\s+(.*)$/);
      if (!match) return null;
      const [, mode, type, sha, size, fullPath] = match;
      return {
        mode,
        type, // 'blob' or 'tree'
        sha,
        size: size === '-' ? 0 : parseInt(size, 10),
        name: path.basename(fullPath),
        path: fullPath
      };
    })
    .filter(Boolean);

  res.json(items);
});

/**
 * Get raw file content / blob viewer
 */
router.get('/repos/:owner/:repo/blob/:branch/*', optionalWebAuth, authorizeGitRead, async (req, res) => {
  const { branch } = req.params;
  const filePath = req.params[0];

  if (!filePath) {
    return res.status(400).json({ error: 'File path required' });
  }

  // Run: git show <branch>:<path>
  const git = spawnSync('git', [
    '--git-dir', req.localRepoPath,
    'show',
    `${branch}:${filePath}`
  ]);

  if (git.status !== 0) {
    return res.status(404).json({ error: 'File not found', details: git.stderr.toString() });
  }

  // Send raw content. We can guess content type based on path.
  const ext = path.extname(filePath).toLowerCase();
  
  if (['.png', '.jpg', '.jpeg', '.gif', '.webp', '.ico'].includes(ext)) {
    res.setHeader('Content-Type', `image/${ext.substring(1)}`);
  } else if (ext === '.svg') {
    res.setHeader('Content-Type', 'image/svg+xml');
  } else if (ext === '.pdf') {
    res.setHeader('Content-Type', 'application/pdf');
  } else {
    res.setHeader('Content-Type', 'text/plain; charset=utf-8');
  }

  res.send(git.stdout);
});

/**
 * Config endpoint for client-side Firebase Auth initialization
 */
router.get('/config', (req, res) => {
  res.json({
    devMode: process.env.DEV_MODE === 'true',
    firebase: {
      apiKey: process.env.FIREBASE_API_KEY || '',
      authDomain: process.env.FIREBASE_AUTH_DOMAIN || '',
      projectId: process.env.FIREBASE_PROJECT_ID || 'git-bucket-79382',
      storageBucket: process.env.FIREBASE_STORAGE_BUCKET || '',
      messagingSenderId: process.env.FIREBASE_MESSAGING_SENDER_ID || '',
      appId: process.env.FIREBASE_APP_ID || ''
    }
  });
});

export default router;
