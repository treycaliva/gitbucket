import { Router } from 'express';
import { spawn, execSync } from 'child_process';
import path from 'path';
import fs from 'fs';
import { 
  acquireLock, 
  releaseLock, 
  getRepositoryMetadata, 
  updateRepositoryMetadata, 
  verifyPAT 
} from './db.js';
import { downloadRepo, uploadRepo, LOCAL_REPOS_ROOT } from './gcs.js';

const router = Router();

// Locate git-http-backend path dynamically
let gitHttpBackendPath;
try {
  const gitExecPath = execSync('git --exec-path').toString().trim();
  gitHttpBackendPath = path.join(gitExecPath, 'git-http-backend');
} catch (err) {
  console.error('Error finding git-http-backend:', err);
  gitHttpBackendPath = '/usr/lib/git-core/git-http-backend'; // Fallback
}

/**
 * Helper to get local sync timestamp file path
 */
function getSyncFilePath(owner, repo) {
  return path.join(LOCAL_REPOS_ROOT, owner.toLowerCase(), `${repo.toLowerCase()}.git`, 'last_sync_timestamp');
}

/**
 * Helper to update local sync timestamp to match firestore updatedAt
 */
function writeLocalSyncTimestamp(owner, repo, updatedAtDoc) {
  try {
    const syncFilePath = getSyncFilePath(owner, repo);
    const ms = updatedAtDoc.toMillis();
    fs.writeFileSync(syncFilePath, ms.toString(), 'utf-8');
  } catch (err) {
    console.error(`Failed to write local sync timestamp for ${owner}/${repo}:`, err);
  }
}

/**
 * Checks if local repository cache is up to date with Firestore updatedAt
 */
function isLocalCacheUpToDate(owner, repo, updatedAtDoc) {
  try {
    const syncFilePath = getSyncFilePath(owner, repo);
    if (!fs.existsSync(syncFilePath)) return false;
    const localMs = fs.readFileSync(syncFilePath, 'utf-8').trim();
    const remoteMs = updatedAtDoc.toMillis().toString();
    return localMs === remoteMs;
  } catch (err) {
    return false;
  }
}

/**
 * Extracts branch list from the bare repository
 */
function getRepoBranches(localRepoPath) {
  try {
    const output = execSync(
      `git --git-dir="${localRepoPath}" for-each-ref --format="%(refname:short)" refs/heads/`,
      { encoding: 'utf-8' }
    );
    const branches = output.split('\n').map(b => b.trim()).filter(Boolean);
    return branches.length > 0 ? branches : ['main'];
  } catch (err) {
    console.error(`Error getting branches for ${localRepoPath}:`, err);
    return ['main'];
  }
}

// ----------------------------------------------------
// GIT SMART HTTP ROUTE
// Handles requests like:
// - GET /r/:owner/:repo.git/info/refs
// - POST /r/:owner/:repo.git/git-upload-pack
// - POST /r/:owner/:repo.git/git-receive-pack
// ----------------------------------------------------
router.all('/:owner/:repo.git/*', async (req, res) => {
  const { owner, repo: repoWithGit } = req.params;
  const repo = repoWithGit.endsWith('.git') ? repoWithGit.slice(0, -4) : repoWithGit;
  
  // Determine if it's a write (push) operation
  const isWrite = req.path.includes('git-receive-pack') || req.query.service === 'git-receive-pack';

  console.log(`[Git HTTP] Request: ${req.method} ${req.originalUrl} (isWrite: ${isWrite})`);

  // 1. Fetch Repository Metadata
  let repoMeta;
  try {
    repoMeta = await getRepositoryMetadata(owner, repo);
  } catch (err) {
    console.error('Error fetching repository metadata:', err);
    return res.status(500).send('Database error');
  }

  if (!repoMeta) {
    return res.status(404).send('Repository not found');
  }

  // 2. Authentication and Authorization Check
  let authenticatedUser = null;
  const authHeader = req.headers.authorization;

  if (authHeader && authHeader.startsWith('Basic ')) {
    const credentials = Buffer.from(authHeader.substring(6), 'base64').toString('utf-8');
    const [username, pat] = credentials.split(':');
    
    try {
      const verified = await verifyPAT(pat);
      if (verified && verified.username.toLowerCase() === username.toLowerCase()) {
        authenticatedUser = verified;
      }
    } catch (err) {
      console.error('Error verifying PAT:', err);
    }
  }

  const isOwner = authenticatedUser && authenticatedUser.username.toLowerCase() === owner.toLowerCase();

  if (isWrite) {
    // Push requires authenticated owner
    if (!isOwner) {
      res.setHeader('WWW-Authenticate', 'Basic realm="GitBucket"');
      return res.status(401).send('Unauthorized. Pushing requires ownership.');
    }
  } else {
    // Pull/Clone: requires auth if repo is private
    if (repoMeta.visibility === 'private' && !isOwner) {
      res.setHeader('WWW-Authenticate', 'Basic realm="GitBucket"');
      return res.status(401).send('Unauthorized. Private repository.');
    }
  }

  // 3. Locking and Syncing Cache
  let lockToken = null;
  const localRepoPath = path.join(LOCAL_REPOS_ROOT, owner.toLowerCase(), `${repo.toLowerCase()}.git`);

  try {
    // Acquire lock if writing, or if reading but cache needs update
    const needsSync = !isLocalCacheUpToDate(owner, repo, repoMeta.updatedAt);
    
    if (isWrite || needsSync) {
      console.log(`[Git HTTP] Acquiring lock for ${owner}/${repo}...`);
      lockToken = await acquireLock(owner, repo);
    }

    // Sync if local cache is missing or outdated
    if (!isLocalCacheUpToDate(owner, repo, repoMeta.updatedAt)) {
      console.log(`[Git HTTP] Cache outdated for ${owner}/${repo}. Syncing from GCS...`);
      const downloaded = await downloadRepo(owner, repo);
      if (!downloaded) {
        // GCS has no archive yet, initialize new bare repo
        console.log(`[Git HTTP] Repo not found in GCS. Initializing new bare repo...`);
        fs.mkdirSync(localRepoPath, { recursive: true });
        execSync(`git init --bare "${localRepoPath}"`);
        execSync(`git -C "${localRepoPath}" config http.receivepack true`);
        execSync(`git -C "${localRepoPath}" config http.uploadpack true`);
        // Set default branch to main
        execSync(`git -C "${localRepoPath}" symbolic-ref HEAD refs/heads/main`);
      }
      writeLocalSyncTimestamp(owner, repo, repoMeta.updatedAt);
    }
  } catch (err) {
    console.error('[Git HTTP] Pre-flight lock/sync error:', err);
    if (lockToken) await releaseLock(owner, repo, lockToken);
    return res.status(500).send('Server synchronization error');
  }

  // 4. CGI Execution of git-http-backend
  console.log(`[Git HTTP] Spawning git-http-backend for ${owner}/${repo}...`);
  
  // Setup environment variables for git-http-backend
  const gitPathSuffix = req.path.split('.git')[1] || '';
  const env = {
    ...process.env,
    GIT_PROJECT_ROOT: LOCAL_REPOS_ROOT,
    GIT_HTTP_EXPORT_ALL: '1',
    PATH_INFO: `/${owner.toLowerCase()}/${repo.toLowerCase()}.git${gitPathSuffix}`,
    REQUEST_METHOD: req.method,
    QUERY_STRING: req.url.split('?')[1] || '',
    CONTENT_TYPE: req.headers['content-type'] || '',
    REMOTE_USER: authenticatedUser ? authenticatedUser.username : '',
    REMOTE_ADDR: req.ip
  };

  const gitProcess = spawn(gitHttpBackendPath, [], { env });

  // Stream request body (from client git command) to git stdin
  req.pipe(gitProcess.stdin);

  // Handle stdout (header parsing and body streaming)
  let headerBuffer = Buffer.alloc(0);
  let headersParsed = false;

  gitProcess.stdout.on('data', (chunk) => {
    if (headersParsed) {
      res.write(chunk);
      return;
    }

    headerBuffer = Buffer.concat([headerBuffer, chunk]);

    let delimiterIndex = headerBuffer.indexOf('\r\n\r\n');
    let delimiterLength = 4;
    if (delimiterIndex === -1) {
      delimiterIndex = headerBuffer.indexOf('\n\n');
      delimiterLength = 2;
    }

    if (delimiterIndex !== -1) {
      headersParsed = true;
      const headerString = headerBuffer.subarray(0, delimiterIndex).toString('utf-8');
      const bodyRemainder = headerBuffer.subarray(delimiterIndex + delimiterLength);

      const lines = headerString.split(/\r?\n/);
      let statusCode = 200;

      for (const line of lines) {
        const colonIndex = line.indexOf(':');
        if (colonIndex !== -1) {
          const key = line.substring(0, colonIndex).trim();
          const val = line.substring(colonIndex + 1).trim();

          if (key.toLowerCase() === 'status') {
            const statusMatch = val.match(/^(\d+)/);
            if (statusMatch) {
              statusCode = parseInt(statusMatch[1], 10);
            }
          } else {
            res.setHeader(key, val);
          }
        }
      }

      res.status(statusCode);
      if (bodyRemainder.length > 0) {
        res.write(bodyRemainder);
      }
    }
  });

  gitProcess.stderr.on('data', (data) => {
    console.error(`[Git HTTP stderr] ${data.toString()}`);
  });

  gitProcess.on('close', async (code) => {
    console.log(`[Git HTTP] git-http-backend process closed with code ${code}`);
    
    try {
      if (code === 0) {
        res.end();
        
        // 5. If it was a push, archive the new state, upload to GCS, and update Firestore
        if (isWrite) {
          console.log(`[Git HTTP] Write operation completed successfully. Archiving to GCS...`);
          await uploadRepo(owner, repo);
          
          const branches = getRepoBranches(localRepoPath);
          await updateRepositoryMetadata(owner, repo, {
            branches,
            // updatedAt is updated inside updateRepositoryMetadata
          });

          // Fetch the updated repository metadata to sync the timestamp locally
          const updatedMeta = await getRepositoryMetadata(owner, repo);
          writeLocalSyncTimestamp(owner, repo, updatedMeta.updatedAt);
          console.log(`[Git HTTP] GCS sync and Firestore metadata update complete!`);
        }
      } else {
        if (!res.headersSent) {
          res.status(500).send('Git backend error');
        } else {
          res.end();
        }
      }
    } catch (err) {
      console.error('[Git HTTP] Post-execution sync/release error:', err);
    } finally {
      // 6. Release lock
      if (lockToken) {
        console.log(`[Git HTTP] Releasing lock for ${owner}/${repo}...`);
        await releaseLock(owner, repo, lockToken);
      }
    }
  });

  gitProcess.on('error', async (err) => {
    console.error('[Git HTTP] Process spawn error:', err);
    if (!res.headersSent) {
      res.status(500).send('CGI Spawn error');
    }
    if (lockToken) {
      await releaseLock(owner, repo, lockToken);
    }
  });
});

export default router;
