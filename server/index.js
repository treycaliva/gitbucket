import express from 'express';
import cors from 'cors';
import dotenv from 'dotenv';
import path from 'path';
import { fileURLToPath } from 'url';
import gitHttpRouter from './git-http.js';
import apiRouter from './api.js';

// Setup environment variables
dotenv.config();

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

const app = express();
const PORT = process.env.PORT || 8080;

// IP Gating Middleware
app.use((req, res, next) => {
  const restrictedIp = process.env.RESTRICTED_IP;
  if (restrictedIp) {
    const xForwardedFor = req.headers['x-forwarded-for'];
    const clientIp = xForwardedFor ? xForwardedFor.split(',').pop().trim() : req.socket.remoteAddress;
    
    let cleanIp = clientIp;
    if (cleanIp && cleanIp.startsWith('::ffff:')) {
      cleanIp = cleanIp.substring(7);
    }

    const isLocal = !cleanIp || cleanIp === '127.0.0.1' || cleanIp === '::1' || cleanIp === 'localhost';

    if (cleanIp !== restrictedIp && !isLocal) {
      console.warn(`[IP Blocked] Request from unauthorized IP: ${cleanIp} (Expected: ${restrictedIp})`);
      return res.status(403).send('Forbidden: Access is restricted to authorized IP addresses.');
    }
  }
  next();
});

// Enable CORS for local dev server
app.use(cors({
  origin: '*', // Customize this in production if desired
  methods: ['GET', 'POST', 'DELETE', 'PUT', 'OPTIONS'],
  allowedHeaders: ['Content-Type', 'Authorization']
}));

// Express JSON body parser - ONLY apply to JSON API routes, NOT Git routes!
// This is critical, as Git requests must be streamed as binary raw data to/from the CGI program.
app.use('/api', express.json());
app.use('/api', apiRouter);

// Register Git smart HTTP middleware under /r
app.use('/r', gitHttpRouter);

// Serve static frontend files in production
const frontendDistPath = path.join(__dirname, '../frontend/dist');
app.use(express.static(frontendDistPath));

// Fallback all other client-side routing to index.html for SPA behavior
app.get('*', (req, res, next) => {
  if (req.path.startsWith('/api') || req.path.startsWith('/r')) {
    return next();
  }
  res.sendFile(path.join(frontendDistPath, 'index.html'), (err) => {
    if (err) {
      // If index.html doesn't exist yet (not built), return a simple welcome message
      res.status(200).send(`
        <!DOCTYPE html>
        <html>
        <head>
          <title>GitBucket API Server</title>
          <style>
            body { font-family: sans-serif; background: #0d1117; color: #c9d1d9; display: flex; align-items: center; justify-content: center; height: 100vh; margin: 0; }
            div { text-align: center; border: 1px solid #30363d; padding: 40px; border-radius: 12px; background: #161b22; box-shadow: 0 8px 24px rgba(0,0,0,0.5); }
            h1 { color: #58a6ff; }
          </style>
        </head>
        <body>
          <div>
            <h1>GitBucket</h1>
            <p>API Server is running successfully.</p>
            <p style="color: #8b949e;">Frontend assets are not yet compiled.</p>
          </div>
        </body>
        </html>
      `);
    }
  });
});

// Start Server
app.listen(PORT, () => {
  console.log(`=============================================`);
  console.log(`🚀 GitBucket server running on port ${PORT}`);
  console.log(`=============================================`);
});
