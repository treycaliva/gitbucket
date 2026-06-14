import path from 'path';
import fs from 'fs';
import { LOCAL_REPOS_ROOT } from './gcs.js';

export function getSyncFilePath(owner, repo) {
  return path.join(LOCAL_REPOS_ROOT, owner.toLowerCase(), `${repo.toLowerCase()}.git`, 'last_sync_timestamp');
}

export function writeLocalSyncTimestamp(owner, repo, updatedAtDoc) {
  try {
    const syncFilePath = getSyncFilePath(owner, repo);
    const ms = updatedAtDoc.toMillis();
    fs.writeFileSync(syncFilePath, ms.toString(), 'utf-8');
  } catch (err) {
    console.error(`Failed to write local sync timestamp for ${owner}/${repo}:`, err);
  }
}

export function isLocalCacheUpToDate(owner, repo, updatedAtDoc) {
  try {
    const syncFilePath = getSyncFilePath(owner, repo);
    if (!fs.existsSync(syncFilePath)) return false;
    const localMs = fs.readFileSync(syncFilePath, 'utf-8').trim();
    const remoteMs = updatedAtDoc.toMillis().toString();
    return localMs === remoteMs;
  } catch (err) {
    console.error(`Failed to check local cache status for ${owner}/${repo}:`, err);
    return false;
  }
}
