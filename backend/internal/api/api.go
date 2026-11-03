// Package api implements the /api/v1 HTTP handlers: authentication, tenancy, RBAC, tokens, audit.
package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Ervisio/ErvisioLogs/backend/internal/auth"
	"github.com/Ervisio/ErvisioLogs/backend/internal/config"
	"github.com/Ervisio/ErvisioLogs/backend/internal/httpx"
	"github.com/Ervisio/ErvisioLogs/backend/internal/oauth"
	"github.com/Ervisio/ErvisioLogs/backend/internal/store"
)

const (
	SessionCookie = "ervisio_logs_session"
	CSRFHeader    = "X-CSRF-Token"
	maxJSONBody   = 64 << 10
	// purgeInterval spaces the opportunistic deletion of expired sessions and OAuth states.
	purgeInterval = 10 * time.Minute
)

// API holds the dependencies of all handlers.
type API struct {
	Store     *store.Store
	Hasher    *auth.Hasher
	Cfg       config.Auth
	Providers map[string]oauth.Provider
	Log       *slog.Logger
	// TrustProxy makes the client IP come from the last X-Forwarded-For entry (set by your reverse proxy).
	TrustProxy bool
	// Logs enables ingest, search and tail. Nil in tests that do not need them.
	Logs *Logs

	catalog   catalog
	tailCount atomic.Int64

	setupMu   sync.Mutex
	setupCode string
	secret    []byte
	secure    bool
	origin    string
	loginIP   *auth.Limiter
	// loginAccountIP stops guessing against an account from one client. loginAccountGlobal is higher and
	// stops distributed guessing; only the former is cleared by a successful login, and one client
	// stops feeding the latter once throttled, so a single IP cannot lock the owner out.
	loginAccountIP     *auth.Limiter
	loginAccountGlobal *auth.Limiter
	oauthCallbackIP    *auth.Limiter
	registerIP         *auth.Limiter
	countUsers         func(context.Context) (int, error)
	lastPurge          atomic.Int64 // unix seconds
	now                func() time.Time
}

// New builds the API. Call EnsureSetupCode before serving.
func New(s *store.Store, h *auth.Hasher, cfg config.Auth, providers map[string]oauth.Provider, log *slog.Logger) *API {
	u, _ := url.Parse(cfg.PublicURL)
	return &API{
		Store: s, Hasher: h, Cfg: cfg, Providers: providers, Log: log,
		setupCode: cfg.SetupCode, secret: []byte(cfg.SecretKey),
		secure: u != nil && u.Scheme == "https", origin: strings.TrimRight(cfg.PublicURL, "/"),
		loginIP:            auth.NewLimiter(30, time.Minute),
		loginAccountIP:     auth.NewLimiter(10, 15*time.Minute),
		loginAccountGlobal: auth.NewLimiter(100, 15*time.Minute),
		oauthCallbackIP:    auth.NewLimiter(30, time.Minute),
		registerIP:         auth.NewLimiter(10, time.Hour),
		countUsers:         s.CountUsers,
		now:                time.Now,
	}
}

// EnsureSetupCode makes sure a first-run setup code exists while there is no user, and logs it once.
func (a *API) EnsureSetupCode(ctx context.Context) error {
	n, err := a.Store.CountUsers(ctx)
	if err != nil || n > 0 {
		return err
	}
	a.setupMu.Lock()
	defer a.setupMu.Unlock()
	if a.setupCode == "" {
		a.setupCode = auth.NewRandom(18)
		a.Log.Warn("first run: no users exist. Use this one-time setup code to create the instance admin account",
			"setup_code", a.setupCode)
	} else {
		a.Log.Warn("first run: no users exist. Create the instance admin account with the configured setup code")
	}
	return nil
}

// setupCodeMatches compares in constant time. It creates the code on demand if start-up could not
// (database not migrated yet), so a fresh install never ends up with no way to create the first admin.
func (a *API) setupCodeMatches(ctx context.Context, given string) bool {
	_ = a.EnsureSetupCode(ctx)
	a.setupMu.Lock()
	defer a.setupMu.Unlock()
	return a.setupCode != "" && subtle.ConstantTimeCompare([]byte(given), []byte(a.setupCode)) == 1
}

func (a *API) clearSetupCode() {
	a.setupMu.Lock()
	a.setupCode = ""
	a.setupMu.Unlock()
}

// Routes mounts the API under the given router (expected at /api/v1).
func (a *API) Routes(r chi.Router) {
	r.Use(a.authenticate)

	r.Get("/auth/providers", a.listProviders)
	r.Post("/auth/register", a.register)
	r.Post("/auth/login", a.login)
	r.Post("/auth/logout", a.logout)
	r.Get("/auth/me", a.me)
	r.Get("/auth/oauth/{provider}/start", a.oauthStart)
	r.Get("/auth/oauth/{provider}/callback", a.oauthCallback)

	r.Get("/workspaces", a.listWorkspaces)
	r.Post("/workspaces", a.createWorkspace)
	r.Get("/workspaces/{id}", a.getWorkspace)
	r.Patch("/workspaces/{id}", a.updateWorkspace)
	r.Delete("/workspaces/{id}", a.deleteWorkspace)
	r.Get("/workspaces/{id}/members", a.listMembers)
	r.Post("/workspaces/{id}/members", a.addMember)
	r.Patch("/workspaces/{id}/members/{userId}", a.updateMember)
	r.Delete("/workspaces/{id}/members/{userId}", a.removeMember)
	r.Get("/workspaces/{id}/audit", a.listAudit)

	r.Get("/projects", a.listProjects)
	r.Post("/projects", a.createProject)
	r.Get("/projects/{id}", a.getProject)
	r.Patch("/projects/{id}", a.updateProject)
	r.Delete("/projects/{id}", a.deleteProject)
	r.Get("/projects/{id}/environments", a.listEnvironments)
	r.Post("/projects/{id}/environments", a.createEnvironment)
	r.Delete("/projects/{id}/environments/{envId}", a.deleteEnvironment)
	r.Get("/projects/{id}/streams", a.listStreams)
	r.Post("/projects/{id}/streams", a.createStream)
	r.Delete("/projects/{id}/streams/{streamId}", a.deleteStream)

	r.Post("/ingest", a.ingestOne)
	r.Post("/ingest/batch", a.ingestBatch)
	r.Post("/logs/search", a.searchLogs)
	r.Get("/logs/tail", a.tailLogs)

	r.Get("/tokens", a.listTokens)
	r.Post("/tokens", a.createToken)
	r.Get("/tokens/current", a.currentToken)
	r.Delete("/tokens/{id}", a.revokeToken)

	r.Get("/admin/users", a.adminListUsers)
	r.Post("/admin/users", a.adminCreateUser)
	r.Patch("/admin/users/{id}", a.adminUpdateUser)
	r.Get("/admin/audit", a.adminListAudit)
}

// ---------- principal ----------

type principal struct {
	user    *store.User
	session *store.Session
	token   *store.APIToken
}

type ctxKey struct{}

func principalOf(r *http.Request) principal {
	p, _ := r.Context().Value(ctxKey{}).(principal)
	return p
}

// authenticate resolves the caller from a Bearer API token or the session cookie. It never rejects
// by itself: handlers decide what they require. Cookie-authenticated unsafe requests must pass the
// CSRF and Origin checks here, before any handler runs.
func (a *API) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var p principal
		if h := r.Header.Get("Authorization"); h != "" {
			tok, ok := strings.CutPrefix(h, "Bearer ")
			if !ok || !auth.LooksLikeAPIToken(tok) {
				httpx.WriteProblem(w, r, http.StatusUnauthorized, "invalid credentials")
				return
			}
			t, err := a.Store.TokenByHash(r.Context(), auth.HashAPIToken(a.secret, tok))
			if err != nil {
				if !errors.Is(err, store.ErrNotFound) {
					a.internalError(w, r, err)
					return
				}
				httpx.WriteProblem(w, r, http.StatusUnauthorized, "invalid credentials")
				return
			}
			p.token = &t
		} else if c, err := r.Cookie(SessionCookie); err == nil && c.Value != "" {
			s, err := a.Store.SessionByHash(r.Context(), auth.HashSession(c.Value))
			switch {
			case err == nil:
				p.session, p.user = &s, &s.User
			case errors.Is(err, store.ErrNotFound):
				// expired or revoked: behave as anonymous (and drop the stale cookie)
				a.clearCookie(w)
			default:
				a.internalError(w, r, err)
				return
			}
		}
		if isUnsafe(r.Method) && (p.session != nil || r.Header.Get("Authorization") == "") {
			if !a.originOK(r) {
				httpx.WriteProblem(w, r, http.StatusForbidden, "cross-origin request refused")
				return
			}
			if p.session != nil && subtle.ConstantTimeCompare([]byte(r.Header.Get(CSRFHeader)), []byte(p.session.CSRFToken)) != 1 {
				httpx.WriteProblem(w, r, http.StatusForbidden, "missing or invalid CSRF token")
				return
			}
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, p)))
	})
}

func isUnsafe(m string) bool {
	return m != http.MethodGet && m != http.MethodHead && m != http.MethodOptions
}

// originOK rejects browser requests coming from another origin. Requests without Origin
// (curl, server-side clients) are allowed: they cannot ride on a victim's cookies.
func (a *API) originOK(r *http.Request) bool {
	o := r.Header.Get("Origin")
	return o == "" || strings.TrimRight(o, "/") == a.origin
}

// requireUser answers 401 unless a human session is present.
func (a *API) requireUser(w http.ResponseWriter, r *http.Request) (*store.User, bool) {
	p := principalOf(r)
	if p.user == nil {
		httpx.WriteProblem(w, r, http.StatusUnauthorized, "authentication required")
		return nil, false
	}
	return p.user, true
}

func (a *API) requireAdmin(w http.ResponseWriter, r *http.Request) (*store.User, bool) {
	u, ok := a.requireUser(w, r)
	if !ok {
		return nil, false
	}
	if !u.IsInstanceAdmin {
		httpx.WriteProblem(w, r, http.StatusForbidden, "instance administrator required")
		return nil, false
	}
	return u, true
}

// workspaceAccess authorises a session user inside a workspace. A non-member gets 404, exactly
// like a missing workspace, so existence is not revealed. perm == "" means any member.
func (a *API) workspaceAccess(w http.ResponseWriter, r *http.Request, user *store.User, ws uuid.UUID, perm string) (store.Membership, bool) {
	m, err := a.Store.MembershipOf(r.Context(), ws, user.ID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			httpx.WriteProblem(w, r, http.StatusNotFound, "not found")
		} else {
			a.internalError(w, r, err)
		}
		return m, false
	}
	if perm != "" && !m.Perms.Has(perm) {
		httpx.WriteProblem(w, r, http.StatusForbidden, "missing permission "+perm)
		return m, false
	}
	return m, true
}

// ---------- helpers ----------

func (a *API) internalError(w http.ResponseWriter, r *http.Request, err error) {
	a.Log.Error("request failed", "request_id", httpx.RequestID(r.Context()), "path", r.URL.Path, "error", err)
	httpx.WriteProblem(w, r, http.StatusInternalServerError, "internal error")
}

// storeError maps store sentinel errors to HTTP answers.
func (a *API) storeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		httpx.WriteProblem(w, r, http.StatusNotFound, "not found")
	case errors.Is(err, store.ErrConflict):
		httpx.WriteProblem(w, r, http.StatusConflict, "already exists")
	case errors.Is(err, store.ErrLastOwner):
		httpx.WriteProblem(w, r, http.StatusConflict, "a workspace needs at least one owner")
	case errors.Is(err, store.ErrForbidden):
		httpx.WriteProblem(w, r, http.StatusForbidden, "you are not allowed to change this member")
	case errors.Is(err, store.ErrBadCursor):
		httpx.WriteProblem(w, r, http.StatusBadRequest, "invalid cursor")
	default:
		a.internalError(w, r, err)
	}
}

// hashFailed answers a password hashing failure: 503 when the hasher is saturated.
func (a *API) hashFailed(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, auth.ErrBusy) {
		w.Header().Set("Retry-After", "1")
		httpx.WriteProblem(w, r, http.StatusServiceUnavailable, "the server is busy, try again shortly")
		return
	}
	a.internalError(w, r, err)
}

// keyedDigest is a keyed, domain-separated digest of value, safe to store or send to the client.
func (a *API) keyedDigest(domain, value string) string {
	m := hmac.New(sha256.New, a.secret)
	m.Write([]byte(domain + "\x00" + value))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

// purgeExpired deletes expired sessions and OAuth states, at most once per purgeInterval. It runs on
// the paths that create them, so the tables stay bounded by the sign-in rate. Failure is not fatal.
func (a *API) purgeExpired(ctx context.Context) {
	now, last := a.now().Unix(), a.lastPurge.Load()
	if now-last < int64(purgeInterval/time.Second) || !a.lastPurge.CompareAndSwap(last, now) {
		return
	}
	if err := a.Store.PurgeExpired(ctx); err != nil {
		a.Log.Warn("purge of expired sessions failed", "error", err)
	}
}

// decode reads a JSON object strictly (unknown fields refused, size capped).
func decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBody)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			httpx.WriteProblem(w, r, http.StatusRequestEntityTooLarge, "request body too large")
		} else {
			httpx.WriteProblem(w, r, http.StatusBadRequest, "malformed JSON body")
		}
		return false
	}
	if _, err := dec.Token(); err != io.EOF {
		httpx.WriteProblem(w, r, http.StatusBadRequest, "unexpected data after JSON body")
		return false
	}
	return true
}

func invalid(w http.ResponseWriter, r *http.Request, msg string) {
	httpx.WriteProblem(w, r, http.StatusUnprocessableEntity, msg)
}

func idParam(w http.ResponseWriter, r *http.Request, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, name))
	if err != nil {
		httpx.WriteProblem(w, r, http.StatusNotFound, "not found")
		return uuid.Nil, false
	}
	return id, true
}

func items(w http.ResponseWriter, v any) {
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": v})
}

func (a *API) clientIP(r *http.Request) string {
	if a.TrustProxy {
		if ip, ok := lastForwardedIP(r.Header.Values("X-Forwarded-For")); ok {
			return ip
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		return addr.Unmap().WithZone("").String()
	}
	return host
}

// lastForwardedIP returns the last valid address across all X-Forwarded-For lines: proxies append,
// so entries further left are client-controlled.
func lastForwardedIP(lines []string) (string, bool) {
	parts := strings.Split(strings.Join(lines, ","), ",")
	for i := len(parts) - 1; i >= 0; i-- {
		if addr, err := netip.ParseAddr(strings.TrimSpace(parts[i])); err == nil {
			return addr.Unmap().WithZone("").String(), true
		}
	}
	return "", false
}

// rateIP is the rate limit key of the client: its address, or its /64 for IPv6.
func (a *API) rateIP(r *http.Request) string {
	return auth.RateKeyIP(a.clientIP(r))
}

// audit records a security-relevant event. A failure to write it is logged, not fatal.
func (a *API) audit(r *http.Request, ws *uuid.UUID, action, targetType, targetID string, detail map[string]any) {
	e := store.AuditEvent{WorkspaceID: ws, Action: action, TargetType: targetType, TargetID: targetID,
		RequestID: httpx.RequestID(r.Context()), IP: a.clientIP(r)}
	if p := principalOf(r); p.user != nil {
		e.ActorUserID = &p.user.ID
	} else if p.token != nil {
		e.ActorToken = &p.token.ID
	}
	if detail != nil {
		e.Detail, _ = json.Marshal(detail)
	}
	if err := a.Store.RecordAudit(r.Context(), e); err != nil {
		a.Log.Error("audit write failed", "action", action, "error", err)
	}
}

func queryInt(r *http.Request, name string, def, max int) int {
	if v, err := strconv.Atoi(r.URL.Query().Get(name)); err == nil && v > 0 {
		if v > max {
			return max
		}
		return v
	}
	return def
}
