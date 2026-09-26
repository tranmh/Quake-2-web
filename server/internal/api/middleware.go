package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"quake2web/server/internal/db"
)

type ctxKey int

const (
	ctxReqInfo ctxKey = iota
	ctxUser
)

// reqInfo is shared between the outer middlewares and the route wrapper.
type reqInfo struct {
	id    string
	route string
}

// RequestID returns the request id of a request handled by the router.
func RequestID(ctx context.Context) string {
	if ri, ok := ctx.Value(ctxReqInfo).(*reqInfo); ok {
		return ri.id
	}
	return ""
}

func requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-Id")
		if id == "" || len(id) > 64 {
			var b [8]byte
			rand.Read(b[:]) //nolint:errcheck
			id = hex.EncodeToString(b[:])
		}
		w.Header().Set("X-Request-Id", id)
		ri := &reqInfo{id: id, route: "unmatched"}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxReqInfo, ri)))
	})
}

// route labels the request with its mux pattern (Go 1.22 has no
// Request.Pattern) for logs and metrics.
func (s *server) route(pattern string, fn http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ri, ok := r.Context().Value(ctxReqInfo).(*reqInfo); ok {
			ri.route = pattern
		}
		fn(w, r)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (w *statusWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += int64(n)
	return n, err
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// Flush implements http.Flusher.
func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

type httpMetrics struct {
	requests *prometheus.CounterVec
	duration *prometheus.HistogramVec
	ingest   *prometheus.CounterVec
}

func newHTTPMetrics(reg *prometheus.Registry) *httpMetrics {
	m := &httpMetrics{
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "q2_http_requests_total", Help: "HTTP requests by route and status code.",
		}, []string{"method", "route", "code"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "q2_http_request_duration_seconds", Help: "HTTP request latency by route.",
			Buckets: prometheus.DefBuckets,
		}, []string{"route"}),
		ingest: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "q2_ingest_jobs", Help: "Pak ingest jobs by final status.",
		}, []string{"status"}),
	}
	for _, c := range []prometheus.Collector{m.requests, m.duration, m.ingest} {
		if err := reg.Register(c); err != nil {
			if are, ok := err.(prometheus.AlreadyRegisteredError); ok {
				switch v := are.ExistingCollector.(type) {
				case *prometheus.CounterVec:
					if c == m.requests {
						m.requests = v
					} else {
						m.ingest = v
					}
				case *prometheus.HistogramVec:
					m.duration = v
				}
			}
		}
	}
	return m
}

// NewRegistry returns a Prometheus registry with Go and process collectors.
func NewRegistry() *prometheus.Registry {
	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	return reg
}

func (s *server) metricsHandler() http.Handler {
	return promhttp.HandlerFor(s.Metrics, promhttp.HandlerOpts{})
}

func (s *server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(sw, r)
		if sw.status == 0 {
			sw.status = http.StatusOK
		}
		ri, _ := r.Context().Value(ctxReqInfo).(*reqInfo)
		route := "unmatched"
		id := ""
		if ri != nil {
			route, id = ri.route, ri.id
		}
		d := time.Since(start)
		s.metrics.requests.WithLabelValues(r.Method, route, strconv.Itoa(sw.status)).Inc()
		s.metrics.duration.WithLabelValues(route).Observe(d.Seconds())
		if route == "GET /healthz" || route == "GET /metrics" {
			return
		}
		lvl := s.Log.Info
		if sw.status >= 500 {
			lvl = s.Log.Error
		} else if strings.HasPrefix(route, "GET /assets/") && sw.status < 400 {
			lvl = s.Log.Debug
		}
		lvl("http request", "req_id", id, "method", r.Method, "path", r.URL.Path, "route", route,
			"status", sw.status, "bytes", sw.bytes, "duration_ms", float64(d.Microseconds())/1000,
			"ip", s.clientIP(r))
	})
}

func (s *server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				s.Log.Error("panic in handler", "req_id", RequestID(r.Context()), "panic", fmt.Sprint(v),
					"stack", string(debug.Stack()))
				writeError(w, http.StatusInternalServerError, "internal", "internal server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// allowedOrigin reports whether a browser origin may call the API with
// credentials: the configured origins (Next.js dev server) or the API's own
// origin.
func (s *server) allowedOrigin(r *http.Request, origin string) bool {
	if origin == "" {
		return false
	}
	for _, o := range s.Config.CORSOrigins {
		if strings.EqualFold(o, origin) {
			return true
		}
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	host := r.Host
	if fh := r.Header.Get("X-Forwarded-Host"); fh != "" && s.Config.TrustProxy {
		host = fh
	}
	return strings.EqualFold(u.Host, host)
}

func (s *server) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && s.allowedOrigin(r, origin) {
			h := w.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			h.Set("Access-Control-Allow-Credentials", "true")
			h.Add("Vary", "Origin")
			h.Set("Access-Control-Expose-Headers", "ETag, X-Request-Id, Content-Range, Accept-Ranges, Content-Length")
			if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
				h.Set("Access-Control-Allow-Methods", "GET, HEAD, POST, PUT, DELETE, OPTIONS")
				h.Set("Access-Control-Allow-Headers", "Content-Type, Range, If-None-Match, X-Request-Id")
				h.Set("Access-Control-Max-Age", "600")
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// csrf rejects cross-site state-changing requests. Together with
// SameSite=Lax session cookies and JSON-only request bodies this makes
// classic form-POST CSRF impossible: unsafe methods must come from an
// allowed Origin (or from a non-browser client that sends no Origin and no
// "Sec-Fetch-Site: cross-site").
func (s *server) csrf(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			if !s.allowedOrigin(r, origin) {
				writeError(w, http.StatusForbidden, "csrf", "cross-origin request rejected")
				return
			}
		} else if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
			writeError(w, http.StatusForbidden, "csrf", "cross-site request rejected")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// SessionCookie is the name of the session cookie.
const SessionCookie = "q2session"

type userCtx struct {
	user  db.User
	token string
}

func (s *server) withUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(SessionCookie)
		if err != nil || c.Value == "" || s.Auth == nil {
			next.ServeHTTP(w, r)
			return
		}
		_, u, err := s.Auth.Authenticate(r.Context(), c.Value)
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}
		ctx := context.WithValue(r.Context(), ctxUser, &userCtx{user: u, token: c.Value})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// UserFromContext returns the authenticated user of a request, if any.
func UserFromContext(ctx context.Context) (db.User, bool) {
	if uc, ok := ctx.Value(ctxUser).(*userCtx); ok {
		return uc.user, true
	}
	return db.User{}, false
}

func currentUser(r *http.Request) (db.User, bool) { return UserFromContext(r.Context()) }

func userID(r *http.Request) int64 {
	if u, ok := currentUser(r); ok {
		return u.ID
	}
	return 0
}

// requireUser writes 401 and returns false when not logged in.
func requireUser(w http.ResponseWriter, r *http.Request) (db.User, bool) {
	u, ok := currentUser(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "login required")
	}
	return u, ok
}

func (s *server) clientIP(r *http.Request) string {
	if s.Config.TrustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			return strings.TrimSpace(parts[len(parts)-1])
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
