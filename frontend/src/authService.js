import { initializeApp } from 'firebase/app';
import { 
  getAuth, 
  signInWithEmailAndPassword as fbSignIn, 
  createUserWithEmailAndPassword as fbSignUp, 
  signOut as fbSignOut, 
  onAuthStateChanged as fbOnAuthStateChanged
} from 'firebase/auth';

class AuthService {
  constructor() {
    this.app = null;
    this.auth = null;
    this.config = null;
    this.devMode = false;
    this.listeners = new Set();
    this.currentUser = null;
    this.initialized = false;
  }

  async init() {
    if (this.initialized) return;

    try {
      const res = await fetch('/api/config');
      this.config = await res.json();
      this.devMode = this.config.devMode;

      if (this.devMode) {
        console.log('[Auth] Running in Mock Developer Auth Mode');
        // Restore mock session from localStorage
        const stored = localStorage.getItem('gitbucket_mock_user');
        if (stored) {
          this.currentUser = JSON.parse(stored);
        }
      } else {
        console.log('[Auth] Initializing Firebase Auth');
        this.app = initializeApp(this.config.firebase);
        this.auth = getAuth(this.app);
        
        fbOnAuthStateChanged(this.auth, async (fbUser) => {
          if (fbUser) {
            // Fetch username from server
            const token = await fbUser.getIdToken();
            try {
              const res = await fetch('/api/user/me', {
                headers: { 'Authorization': `Bearer ${token}` }
              });
              if (res.ok) {
                const userProfile = await res.json();
                this.currentUser = {
                  uid: fbUser.uid,
                  email: fbUser.email,
                  username: userProfile.username
                };
              } else {
                this.currentUser = {
                  uid: fbUser.uid,
                  email: fbUser.email,
                  username: null
                };
              }
            } catch (err) {
              console.error('Error fetching user profile:', err);
              this.currentUser = {
                uid: fbUser.uid,
                email: fbUser.email,
                username: null
              };
            }
          } else {
            this.currentUser = null;
          }
          this._notifyListeners();
        });
      }
    } catch (err) {
      console.error('Failed to initialize auth service:', err);
      // Fallback to devMode locally if config call fails
      this.devMode = true;
    }

    this.initialized = true;
    if (this.devMode) {
      this._notifyListeners();
    }
  }

  onAuthStateChanged(callback) {
    this.listeners.add(callback);
    // Trigger immediately if already loaded
    if (this.initialized) {
      callback(this.currentUser);
    }
    return () => this.listeners.delete(callback);
  }

  _notifyListeners() {
    for (const listener of this.listeners) {
      listener(this.currentUser);
    }
  }

  async login(email, password) {
    await this.init();

    if (this.devMode) {
      // Mock login: use email prefix as username
      const username = email.split('@')[0].toLowerCase();
      const user = {
        uid: username,
        email: email,
        username: username
      };

      // Register username in Firestore mock db by making server call
      const token = `mock_${username}`;
      try {
        // Try to fetch profile, if not registered, register it
        const res = await fetch('/api/user/me', {
          headers: { 'Authorization': `Bearer ${token}` }
        });
        
        if (!res.ok) {
          // Register mock username on server
          await fetch('/api/user/username', {
            method: 'POST',
            headers: { 
              'Content-Type': 'application/json',
              'Authorization': `Bearer ${token}`
            },
            body: JSON.stringify({ username })
          });
        }
      } catch (err) {
        console.error('Mock server setup warning:', err);
      }

      this.currentUser = user;
      localStorage.setItem('gitbucket_mock_user', JSON.stringify(user));
      this._notifyListeners();
      return user;
    } else {
      const cred = await fbSignIn(this.auth, email, password);
      // Username will be fetched in onAuthStateChanged
      return cred.user;
    }
  }

  async signup(email, password, username) {
    await this.init();

    if (this.devMode) {
      const usernameClean = username.toLowerCase();
      const user = {
        uid: usernameClean,
        email: email,
        username: usernameClean
      };

      // Register mock username on server
      const token = `mock_${usernameClean}`;
      const res = await fetch('/api/user/username', {
        method: 'POST',
        headers: { 
          'Content-Type': 'application/json',
          'Authorization': `Bearer ${token}`
        },
        body: JSON.stringify({ username: usernameClean })
      });

      if (!res.ok) {
        const errData = await res.json();
        throw new Error(errData.error || 'Failed to register username');
      }

      this.currentUser = user;
      localStorage.setItem('gitbucket_mock_user', JSON.stringify(user));
      this._notifyListeners();
      return user;
    } else {
      const cred = await fbSignUp(this.auth, email, password);
      // Wait a moment for session propagation, then set username on backend
      const token = await cred.user.getIdToken();
      const res = await fetch('/api/user/username', {
        method: 'POST',
        headers: { 
          'Content-Type': 'application/json',
          'Authorization': `Bearer ${token}`
        },
        body: JSON.stringify({ username })
      });

      if (!res.ok) {
        const errData = await res.json();
        throw new Error(errData.error || 'Failed to register username');
      }
      
      this.currentUser = {
        uid: cred.user.uid,
        email: cred.user.email,
        username: username.toLowerCase()
      };
      this._notifyListeners();
      return cred.user;
    }
  }

  async logout() {
    await this.init();

    if (this.devMode) {
      this.currentUser = null;
      localStorage.removeItem('gitbucket_mock_user');
      this._notifyListeners();
    } else {
      await fbSignOut(this.auth);
    }
  }

  async getToken() {
    await this.init();

    if (this.devMode) {
      return this.currentUser ? `mock_${this.currentUser.uid}` : '';
    } else {
      if (!this.auth.currentUser) return '';
      return await this.auth.currentUser.getIdToken();
    }
  }
}

export const authService = new AuthService();
