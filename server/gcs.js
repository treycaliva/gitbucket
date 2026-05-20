import { Storage } from '@google-cloud/storage';
import { exec } from 'child_process';
import { promisify } from 'util';
import fs from 'fs';
import path from 'path';

const execAsync = promisify(exec);
const storage = new Storage();

const BUCKET_NAME = process.env.GCS_BUCKET || 'git-bucket-repositories-79382';
const bucket = storage.bucket(BUCKET_NAME);

// Base directory for caching bare repositories locally on the container
export const LOCAL_REPOS_ROOT = process.env.LOCAL_REPOS_ROOT || '/tmp/repos';

/**
 * Ensures a directory exists
 */
function ensureDir(dirPath) {
  if (!fs.existsSync(dirPath)) {
    fs.mkdirSync(dirPath, { recursive: true });
  }
}

/**
 * Downloads a repository from GCS and extracts it locally to LOCAL_REPOS_ROOT
 * Returns true if downloaded, false if the repo doesn't exist in GCS yet.
 */
export async function downloadRepo(owner, repo) {
  const gcsKey = `repos/${owner.toLowerCase()}/${repo.toLowerCase()}.tar.gz`;
  const localRepoPath = path.join(LOCAL_REPOS_ROOT, owner.toLowerCase(), `${repo.toLowerCase()}.git`);
  const localTarPath = path.join(LOCAL_REPOS_ROOT, owner.toLowerCase(), `${repo.toLowerCase()}.tar.gz`);

  // Ensure directories exist
  ensureDir(path.dirname(localRepoPath));
  ensureDir(localRepoPath);

  const file = bucket.file(gcsKey);
  const [exists] = await file.exists();

  if (!exists) {
    // Repository is new
    return false;
  }

  console.log(`Downloading ${gcsKey} to ${localTarPath}...`);
  await file.download({ destination: localTarPath });

  console.log(`Extracting ${localTarPath} to ${localRepoPath}...`);
  // Clean the local repo directory first to avoid merging old files
  await execAsync(`rm -rf "${localRepoPath}"/*`);
  ensureDir(localRepoPath);

  // Extract tarball contents directly into the local repo path
  await execAsync(`tar -xzf "${localTarPath}" -C "${localRepoPath}"`);

  // Clean up the temporary local tar file
  fs.unlinkSync(localTarPath);

  return true;
}

/**
 * Compresses the local repository and uploads it to GCS
 */
export async function uploadRepo(owner, repo) {
  const gcsKey = `repos/${owner.toLowerCase()}/${repo.toLowerCase()}.tar.gz`;
  const localRepoPath = path.join(LOCAL_REPOS_ROOT, owner.toLowerCase(), `${repo.toLowerCase()}.git`);
  const localTarPath = path.join(LOCAL_REPOS_ROOT, owner.toLowerCase(), `${repo.toLowerCase()}.tar.gz`);

  if (!fs.existsSync(localRepoPath)) {
    throw new Error(`Local repo path does not exist: ${localRepoPath}`);
  }

  console.log(`Archiving ${localRepoPath} to ${localTarPath}...`);
  ensureDir(path.dirname(localTarPath));
  
  // Compress everything inside the bare repository folder
  await execAsync(`tar -czf "${localTarPath}" -C "${localRepoPath}" .`);

  console.log(`Uploading ${localTarPath} to gs://${BUCKET_NAME}/${gcsKey}...`);
  await bucket.upload(localTarPath, {
    destination: gcsKey,
    metadata: {
      contentType: 'application/gzip',
      cacheControl: 'no-cache, no-store, must-revalidate',
    }
  });

  // Clean up the temporary local tar file
  fs.unlinkSync(localTarPath);
}

/**
 * Deletes repository files from GCS and local cache
 */
export async function deleteRepo(owner, repo) {
  const gcsKey = `repos/${owner.toLowerCase()}/${repo.toLowerCase()}.tar.gz`;
  const localRepoPath = path.join(LOCAL_REPOS_ROOT, owner.toLowerCase(), `${repo.toLowerCase()}.git`);

  console.log(`Deleting repository ${owner}/${repo} from GCS...`);
  const file = bucket.file(gcsKey);
  const [exists] = await file.exists();
  if (exists) {
    await file.delete();
  }

  if (fs.existsSync(localRepoPath)) {
    console.log(`Deleting local cache for ${owner}/${repo}...`);
    await execAsync(`rm -rf "${localRepoPath}"`);
  }
}
