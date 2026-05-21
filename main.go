package main

import (
	"bytes"
	"context"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	kms "cloud.google.com/go/kms/apiv1"
	secretmanager "cloud.google.com/go/secretmanager/apiv1"
	"cloud.google.com/go/storage"
	firebase "firebase.google.com/go/v4"
	fauth "firebase.google.com/go/v4/auth"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"google.golang.org/api/option"

	"gitbucket/internal/api"
	"gitbucket/internal/apps"
	"gitbucket/internal/auth"
	"gitbucket/internal/config"
	"gitbucket/internal/db"
	"gitbucket/internal/gcs"
	"gitbucket/internal/sync"
)

func getClientIP(r *http.Request) string {
	xff := r.Header.Get("X-Forwarded-For")
	if xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}

func ipGatingMiddleware(restrictedIP string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if restrictedIP == "" {
				next.ServeHTTP(w, r)
				return
			}
			clientIP := getClientIP(r)
			if clientIP == "127.0.0.1" || clientIP == "::1" || clientIP == "::ffff:127.0.0.1" {
				next.ServeHTTP(w, r)
				return
			}
			allowedIPs := strings.Split(restrictedIP, ",")
			allowed := false
			for _, ip := range allowedIPs {
				if clientIP == strings.TrimSpace(ip) {
					allowed = true
					break
				}
			}
			if !allowed {
				http.Error(w, "Forbidden: IP restricted", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// errorLoggerResponseWriter captures status and (for 5xx) the first chunk of
// response body so the errorLogger middleware can log the actual error message
// after the handler returns.
type errorLoggerResponseWriter struct {
	http.ResponseWriter
	status int
	body   bytes.Buffer
}

const errorLoggerBodyLimit = 4096

func (w *errorLoggerResponseWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *errorLoggerResponseWriter) Write(b []byte) (int, error) {
	if w.status >= 500 && w.body.Len() < errorLoggerBodyLimit {
		w.body.Write(b[:min(len(b), errorLoggerBodyLimit-w.body.Len())])
	}
	return w.ResponseWriter.Write(b)
}

// errorLogger logs the response body of any 5xx response. Without this,
// http.Error(w, err.Error(), 500) sends the underlying error to the client
// but leaves no record server-side, which makes prod debugging painful.
func errorLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &errorLoggerResponseWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		if rec.status >= 500 {
			log.Printf("[5xx] %s %s -> %d: %s", r.Method, r.URL.Path, rec.status, strings.TrimSpace(rec.body.String()))
		}
	})
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		} else {
			w.Header().Set("Access-Control-Allow-Origin", "*")
		}

		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS, PATCH")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With, Git-Protocol, Accept")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func main() {
	ctx := context.Background()
	cfg := config.Load()

	// Initialize Firestore Client
	var firestoreClient *firestore.Client
	var err error
	if cfg.ProjectID != "" {
		firestoreClient, err = db.NewClient(ctx, cfg.ProjectID)
		if err != nil {
			log.Fatalf("failed to initialize firestore client: %v", err)
		}
		defer firestoreClient.Close()
		log.Printf("Firestore client initialized for project %s", cfg.ProjectID)
	} else {
		log.Println("Warning: ProjectID is empty, Firestore client not initialized")
	}

	// Initialize Firebase Auth Client
	var authClient *fauth.Client
	fbConf := &firebase.Config{ProjectID: cfg.ProjectID}
	if !cfg.DevMode {
		app, err := firebase.NewApp(ctx, fbConf)
		if err != nil {
			log.Fatalf("failed to initialize firebase app: %v", err)
		}
		authClient, err = app.Auth(ctx)
		if err != nil {
			log.Fatalf("failed to initialize firebase auth client: %v", err)
		}
	} else {
		app, err := firebase.NewApp(ctx, fbConf)
		if err == nil {
			authClient, _ = app.Auth(ctx)
		}
	}

	// Initialize GCS Client
	var storageClient *storage.Client
	if cfg.GCSBucket != "" {
		if os.Getenv("STORAGE_EMULATOR_HOST") != "" {
			storageClient, err = storage.NewClient(ctx, option.WithoutAuthentication())
		} else {
			storageClient, err = gcs.NewClient(ctx)
		}
		if err != nil {
			log.Fatalf("failed to initialize GCS client: %v", err)
		}
		defer storageClient.Close()
	}
	// Initialize Cloud KMS client if configured and not in dev mode
	var kmsKeyMgmtClient *kms.KeyManagementClient
	if cfg.KMSKeyName != "" && !cfg.DevMode {
		kmsKeyMgmtClient, err = kms.NewKeyManagementClient(ctx)
		if err != nil {
			log.Fatalf("failed to initialize Cloud KMS client: %v", err)
		}
		defer kmsKeyMgmtClient.Close()
		log.Printf("Cloud KMS client initialized for key: %s", cfg.KMSKeyName)
	}
	syncKMSClient := sync.NewKMSClient(kmsKeyMgmtClient, cfg.KMSKeyName)

	// SecretStore for the GitHub App emulation subsystem (private keys, webhook secrets).
	var appsSecretStore apps.SecretStore
	if cfg.DevMode {
		appsSecretStore = apps.NewMemorySecretStore()
		log.Println("apps: using in-memory SecretStore (DEV_MODE)")
	} else {
		smClient, err := secretmanager.NewClient(ctx)
		if err != nil {
			log.Fatalf("failed to initialize Secret Manager client: %v", err)
		}
		defer smClient.Close()
		appsSecretStore = apps.NewRealSecretStore(smClient, cfg.SecretManagerProject)
		log.Printf("apps: Secret Manager client initialized for project %s", cfg.SecretManagerProject)
	}

	// Initialize Auth Handler
	authHandler := auth.NewAuthHandler(cfg.DevMode, authClient, firestoreClient)

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(errorLogger)
	r.Use(ipGatingMiddleware(cfg.RestrictedIP))
	r.Use(corsMiddleware)

	// API Routes
	apiHandler := api.NewAPIHandler(firestoreClient, storageClient, cfg.GCSBucket, cfg.LocalReposRoot, authHandler, syncKMSClient)
	apiHandler.RegisterRoutes(r)

	// GitHub App emulation routes (Plan 1: JWT-authed App endpoints only).
	appsJWTVerifier := apps.NewJWTVerifier(firestoreClient, 60*time.Second)
	appsHandler := apps.NewHandler(firestoreClient, appsSecretStore, appsJWTVerifier)
	apps.RegisterRoutes(r, appsHandler)

	// Static Assets & SPA Fallback
	staticDir := "frontend/dist"
	fs := http.FileServer(http.Dir(staticDir))

	r.HandleFunc("/*", func(w http.ResponseWriter, req *http.Request) {
		if strings.HasPrefix(req.URL.Path, "/api/") || strings.HasPrefix(req.URL.Path, "/r/") {
			http.NotFound(w, req)
			return
		}

		path := filepath.Clean(req.URL.Path)
		filePath := filepath.Join(staticDir, path)
		info, err := os.Stat(filePath)
		if err != nil || info.IsDir() {
			http.ServeFile(w, req, filepath.Join(staticDir, "index.html"))
			return
		}

		fs.ServeHTTP(w, req)
	})

	log.Printf("Starting server on port %s...", cfg.Port)
	if err := http.ListenAndServe(":"+cfg.Port, r); err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}
