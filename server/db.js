import admin from 'firebase-admin';
import crypto from 'crypto';

// Initialize Firebase Admin
// In Cloud Run, it automatically uses the default service account credentials.
// For local development, it will look for GOOGLE_APPLICATION_CREDENTIALS or Firebase Emulator.
if (admin.apps.length === 0) {
  admin.initializeApp();
}

export const db = admin.firestore();

// ----------------------------------------------------
// DISTRIBUTED LOCKS
// ----------------------------------------------------

/**
 * Sleeps for specified milliseconds
 */
const sleep = (ms) => new Promise((resolve) => resolve && setTimeout(resolve, ms));

/**
 * Acquires a write lock for a repository.
 * Retries every 500ms for up to timeoutMs.
 */
export async function acquireLock(owner, repo, leaseMs = 30000, timeoutMs = 15000) {
  const lockId = `${owner}_${repo}`;
  const lockRef = db.collection('locks').doc(lockId);
  const token = crypto.randomBytes(16).toString('hex');
  const start = Date.now();

  while (Date.now() - start < timeoutMs) {
    try {
      const acquired = await db.runTransaction(async (transaction) => {
        const doc = await transaction.get(lockRef);
        const now = admin.firestore.Timestamp.now();
        const nowMs = now.toMillis();

        if (doc.exists) {
          const data = doc.data();
          const expiresMs = data.expiresAt.toMillis();
          if (nowMs < expiresMs) {
            // Lock is still active, fail transaction to retry
            return false;
          }
        }

        // Lock does not exist or has expired, acquire it
        const expiresAt = admin.firestore.Timestamp.fromMillis(nowMs + leaseMs);
        transaction.set(lockRef, {
          acquiredBy: token,
          acquiredAt: now,
          expiresAt: expiresAt
        });
        return true;
      });

      if (acquired) {
        return token;
      }
    } catch (err) {
      console.error(`Lock transaction error for ${lockId}:`, err);
    }
    // Wait and retry
    await sleep(500);
  }

  throw new Error(`Timeout acquiring lock for repository ${owner}/${repo}`);
}

/**
 * Releases a lock if the token matches.
 */
export async function releaseLock(owner, repo, token) {
  const lockId = `${owner}_${repo}`;
  const lockRef = db.collection('locks').doc(lockId);

  try {
    await db.runTransaction(async (transaction) => {
      const doc = await transaction.get(lockRef);
      if (doc.exists) {
        const data = doc.data();
        if (data.acquiredBy === token) {
          transaction.delete(lockRef);
        }
      }
    });
  } catch (err) {
    console.error(`Error releasing lock for ${lockId}:`, err);
  }
}

// ----------------------------------------------------
// USERS & PERSONAL ACCESS TOKENS (PATs)
// ----------------------------------------------------

/**
 * Hashes a token for secure storage
 */
function hashToken(token) {
  return crypto.createHash('sha256').update(token).digest('hex');
}

/**
 * Resolves a username to uid, and vice versa
 */
export async function getUidByUsername(username) {
  const snapshot = await db.collection('users')
    .where('username', '==', username.toLowerCase())
    .limit(1)
    .get();
  
  if (snapshot.empty) return null;
  return snapshot.docs[0].id;
}

export async function getUsernameByUid(uid) {
  const doc = await db.collection('users').doc(uid).get();
  if (!doc.exists) return null;
  return doc.data().username;
}

/**
 * Registers/sets up a username for a Firebase Auth user
 */
export async function registerUsername(uid, username, email) {
  const userRef = db.collection('users').doc(uid);
  const usernameLower = username.toLowerCase();

  return await db.runTransaction(async (transaction) => {
    // Check if username is already taken
    const query = db.collection('users').where('username', '==', usernameLower).limit(1);
    const snapshot = await transaction.get(query);
    if (!snapshot.empty) {
      throw new Error(`Username ${username} is already taken.`);
    }

    // Set the user profile doc
    transaction.set(userRef, {
      username: usernameLower,
      displayName: username,
      email: email,
      createdAt: admin.firestore.FieldValue.serverTimestamp()
    });
  });
}

/**
 * Generates a Personal Access Token for CLI access
 */
export async function generatePAT(uid, name) {
  const rawToken = 'gb_pat_' + crypto.randomBytes(24).toString('hex');
  const tokenHash = hashToken(rawToken);

  await db.collection('tokens').add({
    uid,
    name,
    tokenHash,
    createdAt: admin.firestore.FieldValue.serverTimestamp(),
    lastUsedAt: null
  });

  return rawToken;
}

/**
 * Verifies a PAT token and returns the owner's uid & username
 */
export async function verifyPAT(token) {
  const tokenHash = hashToken(token);
  const snapshot = await db.collection('tokens')
    .where('tokenHash', '==', tokenHash)
    .limit(1)
    .get();

  if (snapshot.empty) return null;

  const tokenDoc = snapshot.docs[0];
  const data = tokenDoc.data();
  const username = await getUsernameByUid(data.uid);

  // Async log last usage time
  tokenDoc.ref.update({
    lastUsedAt: admin.firestore.FieldValue.serverTimestamp()
  }).catch(err => console.error('Error updating token lastUsedAt:', err));

  return { uid: data.uid, username };
}

/**
 * Lists PATs for a user (without sensitive hash)
 */
export async function listPATs(uid) {
  const snapshot = await db.collection('tokens')
    .where('uid', '==', uid)
    .get();

  return snapshot.docs.map(doc => ({
    id: doc.id,
    name: doc.data().name,
    createdAt: doc.data().createdAt?.toDate(),
    lastUsedAt: doc.data().lastUsedAt?.toDate()
  }));
}

/**
 * Revokes a PAT by id
 */
export async function revokePAT(uid, tokenId) {
  const docRef = db.collection('tokens').doc(tokenId);
  const doc = await docRef.get();
  if (doc.exists && doc.data().uid === uid) {
    await docRef.delete();
    return true;
  }
  return false;
}

// ----------------------------------------------------
// REPOSITORIES METADATA
// ----------------------------------------------------

export async function createRepositoryMetadata(ownerUid, ownerUsername, repoName, description, visibility) {
  const repoId = `${ownerUsername.toLowerCase()}_${repoName.toLowerCase()}`;
  const repoRef = db.collection('repositories').doc(repoId);

  return await db.runTransaction(async (transaction) => {
    const doc = await transaction.get(repoRef);
    if (doc.exists) {
      throw new Error(`Repository ${ownerUsername}/${repoName} already exists.`);
    }

    transaction.set(repoRef, {
      ownerUid,
      owner: ownerUsername.toLowerCase(),
      name: repoName,
      description: description || '',
      visibility: visibility || 'public', // 'public' or 'private'
      defaultBranch: 'main',
      createdAt: admin.firestore.FieldValue.serverTimestamp(),
      updatedAt: admin.firestore.FieldValue.serverTimestamp(),
      branches: ['main'],
      commitsCache: []
    });
  });
}

export async function getRepositoryMetadata(owner, repo) {
  const repoId = `${owner.toLowerCase()}_${repo.toLowerCase()}`;
  const doc = await db.collection('repositories').doc(repoId).get();
  if (!doc.exists) return null;
  return doc.data();
}

export async function updateRepositoryMetadata(owner, repo, data) {
  const repoId = `${owner.toLowerCase()}_${repo.toLowerCase()}`;
  const repoRef = db.collection('repositories').doc(repoId);
  await repoRef.update({
    ...data,
    updatedAt: admin.firestore.FieldValue.serverTimestamp()
  });
}

export async function deleteRepositoryMetadata(owner, repo) {
  const repoId = `${owner.toLowerCase()}_${repo.toLowerCase()}`;
  await db.collection('repositories').doc(repoId).delete();
}

export async function listUserRepositories(owner) {
  const snapshot = await db.collection('repositories')
    .where('owner', '==', owner.toLowerCase())
    .get();
  return snapshot.docs.map(doc => doc.data());
}

export async function listAllPublicRepositories() {
  const snapshot = await db.collection('repositories')
    .where('visibility', '==', 'public')
    .get();
  return snapshot.docs.map(doc => doc.data());
}
