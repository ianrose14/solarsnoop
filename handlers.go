package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ianrose14/solarsnoop/internal"
	"github.com/ianrose14/solarsnoop/internal/notifications"
	"github.com/ianrose14/solarsnoop/pkg/enphase"
	"github.com/ianrose14/solarsnoop/pkg/httpserver"
)

func (svr *server) loginHandler(rw http.ResponseWriter, r *http.Request) {
	w := httpserver.NewResponseWriterPeeker(rw)
	ctx := r.Context()

	start := time.Now()
	defer requestLog(w, r, start)

	authCode := r.URL.Query().Get("code")
	if authCode == "" {
		http.Error(w, "no code present in querystring", http.StatusBadRequest)
		return
	}

	authRsp, err := svr.enphaseClient.CompleteAuthorization(ctx, authCode, fmt.Sprintf("//%s/oauth/callback", r.Host))
	if err != nil {
		httpError(w, err.Error(), nil, http.StatusInternalServerError)
		return
	}

	log.Printf("successful authorization for user %q", authRsp.UserId)

	systems, err := svr.enphaseClient.FetchSystems(ctx, authRsp.AccessToken)
	if err != nil {
		httpError(w, err.Error(), nil, http.StatusInternalServerError)
		return
	}

	sessionToken, err := internal.UpsertSession(ctx, svr.db, authRsp.UserId, authRsp.AccessToken, authRsp.RefreshToken)
	if err != nil {
		httpError(w, "", err, http.StatusInternalServerError)
		return
	}

	log.Printf("created session %q for user %q", sessionToken, authRsp.UserId)

	err = internal.InsertSystems(ctx, svr.db, authRsp.UserId, systems)
	if err != nil {
		httpError(w, "", err, http.StatusInternalServerError)
		return
	}

	log.Printf("saved %d enphase systems to db", len(systems))

	// TODO - secure=true, samesite=???
	http.SetCookie(w, &http.Cookie{
		Name:    SessionCookieName,
		Value:   SessionCookiePrefix + sessionToken,
		Path:    "/",
		Expires: time.Now().Add(365 * 24 * time.Hour),
	})

	http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
}

func (svr *server) logoutHandler(rw http.ResponseWriter, r *http.Request) {
	w := httpserver.NewResponseWriterPeeker(rw)

	http.SetCookie(w, &http.Cookie{
		Name:    SessionCookieName,
		Value:   "",
		Path:    "/",
		Expires: time.Unix(0, 0),
	})

	http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
}

func (svr *server) addNotificationHandler(rw http.ResponseWriter, r *http.Request) {
	w := httpserver.NewResponseWriterPeeker(rw)
	ctx := r.Context()

	start := time.Now()
	defer requestLog(w, r, start)

	session, systems, err := getCurrentSession(ctx, r, svr.db)
	if err != nil {
		log.Printf("failed to lookup current session: %s", err)
	}
	if session == nil {
		httpError(w, "not logged in", nil, http.StatusUnauthorized)
		return
	}

	if err := r.ParseForm(); err != nil {
		httpError(w, "", err, http.StatusBadRequest)
		return
	}

	systemId, err := readInt64Param(r.Form, "systemId")
	if err != nil {
		httpError(w, err.Error(), nil, http.StatusBadRequest) // this errMsg is always safe to echo to client
		return
	}

	kind := notifications.Kind(strings.ToLower(r.Form.Get("kind")))
	if kind == "" {
		httpError(w, "'kind' is required", nil, http.StatusBadRequest)
		return
	}
	if !kind.IsValid() {
		httpError(w, "invalid 'kind' parameter", nil, http.StatusBadRequest)
		return
	}

	recipient := r.Form.Get("recipient")
	if recipient == "" {
		httpError(w, "'recipient' is required", nil, http.StatusBadRequest)
		return
	}

	// TODO(ianrose): validate recipient based on kind

	found := false
	for _, system := range systems {
		if systemId == system.SystemId {
			found = true
			break
		}
	}
	if !found {
		httpError(w, "invalid 'systemId' parameter", nil, http.StatusBadRequest)
		return
	}

	if err := internal.InsertNotifier(ctx, svr.db, session.UserId, systemId, kind, recipient); err != nil {
		httpError(w, fmt.Sprintf("failed to save new config for system %d", systemId), err, http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
}

func (svr *server) deleteNotificationHandler(rw http.ResponseWriter, r *http.Request) {
	w := httpserver.NewResponseWriterPeeker(rw)
	ctx := r.Context()

	start := time.Now()
	defer requestLog(w, r, start)

	session, _, err := getCurrentSession(ctx, r, svr.db)
	if err != nil {
		log.Printf("failed to lookup current session: %s", err)
	}
	if session == nil {
		httpError(w, "not logged in", nil, http.StatusUnauthorized)
		return
	}

	if err := r.ParseForm(); err != nil {
		httpError(w, "", err, http.StatusBadRequest)
		return
	}

	notifierId, err := readInt64Param(r.Form, "notifierId")
	if err != nil {
		httpError(w, err.Error(), nil, http.StatusBadRequest) // this errMsg is always safe to echo to client
		return
	}

	systemId, err := readInt64Param(r.Form, "systemId")
	if err != nil {
		httpError(w, err.Error(), nil, http.StatusBadRequest) // this errMsg is always safe to echo to client
		return
	}

	if err := internal.DeleteNotifier(ctx, svr.db, session.UserId, systemId, notifierId); err != nil {
		httpError(w, fmt.Sprintf("failed to delete notifier %d", notifierId), err, http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
}

func (svr *server) refreshHandler(rw http.ResponseWriter, r *http.Request) {
	w := httpserver.NewResponseWriterPeeker(rw)
	ctx := r.Context()

	start := time.Now()
	defer requestLog(w, r, start)

	if !strings.HasPrefix(r.Host, "localhost") {
		http.Error(w, "admin handler not authorized", http.StatusUnauthorized)
		return
	}

	conn, err := svr.db.Conn(ctx)
	if err != nil {
		httpError(w, "", fmt.Errorf("failed to get database connection: %w", err), http.StatusInternalServerError)
		return
	}
	defer conn.Close()

	sessions, err := internal.QuerySessions(ctx, svr.db, "" /* all */)
	if err != nil {
		httpError(w, "", fmt.Errorf("failed to query sessions: %w", err), http.StatusInternalServerError)
		return
	}

	for _, session := range sessions {
		rsp, err := svr.enphaseClient.RefreshTokens(ctx, session.RefreshToken)
		if err != nil {
			log.Printf("failed to refresh tokens for session for user %s: %s", session.UserId, err)
			continue
		}

		if _, err := internal.UpsertSession(ctx, svr.db, session.UserId, rsp.AccessToken, rsp.RefreshToken); err != nil {
			log.Printf("failed to upsert session for user %s: %s", session.UserId, err)
			continue
		}
	}

	fmt.Fprintf(w, "ok!")
}

func (svr *server) rootHandler(rw http.ResponseWriter, r *http.Request) {
	w := httpserver.NewResponseWriterPeeker(rw)
	ctx := r.Context()

	start := time.Now()
	defer requestLog(w, r, start)

	type NotifierConfig struct {
		internal.NotifierConfig
		SystemId   int64
		SystemName string
	}

	args := struct {
		UserId        string
		Systems       []*enphase.System
		Notifiers     []NotifierConfig
		OAuthClientId string
		CallbackUrl   string
	}{
		OAuthClientId: enphaseClientId,
		CallbackUrl:   fmt.Sprintf("//%s/oauth/callback", r.Host),
	}

	session, systems, err := getCurrentSession(ctx, r, svr.db)
	if err != nil {
		log.Printf("failed to lookup current session: %s", err)
	}
	if session != nil {
		args.UserId = session.UserId
		args.Systems = systems
		log.Printf("found %d systems for user %s", len(systems), session.UserId)
	}

	for _, system := range systems {
		configs, err := internal.QueryNotifiers(ctx, svr.db, session.UserId, system.SystemId)
		if err != nil {
			httpError(w, fmt.Sprintf("failed to query configured notifications for system %d", system.SystemId), err, http.StatusInternalServerError)
			return
		}
		for _, config := range configs {
			args.Notifiers = append(args.Notifiers, NotifierConfig{
				NotifierConfig: config,
				SystemId:       system.SystemId,
				SystemName:     system.Name,
			})
		}
	}

	err = rootTemplate.Execute(w, &args)
	if err != nil {
		log.Printf("failed to execute template: %s", err)
		w.WriteHeader(http.StatusInternalServerError)
	}
}

func (svr *server) sessionsHandler(rw http.ResponseWriter, r *http.Request) {
	w := httpserver.NewResponseWriterPeeker(rw)
	ctx := r.Context()

	start := time.Now()
	defer requestLog(w, r, start)

	if !strings.HasPrefix(r.Host, "localhost") {
		http.Error(w, "admin handler not authorized", http.StatusUnauthorized)
		return
	}

	conn, err := svr.db.Conn(ctx)
	if err != nil {
		httpError(w, "", fmt.Errorf("failed to get database connection: %w", err), http.StatusInternalServerError)
		return
	}
	defer conn.Close()

	sessions, err := internal.QuerySessions(ctx, svr.db, "" /* all */)
	if err != nil {
		httpError(w, "", fmt.Errorf("failed to query sessions: %w", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("content-type", "text/plain")
	fmt.Fprintf(w, "%d total results:\n", len(sessions))
	for _, session := range sessions {
		fmt.Fprintf(w, "%+v\n", session)
	}
}

func getCurrentSession(ctx context.Context, r *http.Request, db *sql.DB) (*internal.AuthSession, []*enphase.System, error) {
	c, err := r.Cookie(SessionCookieName)
	if err != nil {
		if err != http.ErrNoCookie {
			return nil, nil, fmt.Errorf("failed to read %q cookie: %w", SessionCookieName, err)
		}
		log.Printf("no auth cookie")
		return nil, nil, nil
	}

	if !strings.HasPrefix(c.Value, SessionCookiePrefix) {
		return nil, nil, fmt.Errorf("unrecognized cookie prefix")
	}

	sessionToken := strings.TrimPrefix(c.Value, SessionCookiePrefix)

	sessions, err := internal.QuerySessions(ctx, db, sessionToken)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to query sessions: %w", err)
	}

	if len(sessions) == 0 {
		log.Printf("no sessions found for this token")
		return nil, nil, nil
	}

	systems, err := internal.QuerySystems(ctx, db, sessions[0].UserId)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to query systems: %w", err)
	}

	return sessions[0], systems, nil
}

func httpError(w http.ResponseWriter, msg string, err error, code int) {
	if msg == "" {
		msg = http.StatusText(code)
	}

	prefix := "error"
	if code/100 < 4 {
		prefix = "warning"
	}

	if err != nil {
		log.Printf("%s: %s", prefix, err)
	} else {
		log.Printf("%s: %s", prefix, msg)
	}

	http.Error(w, msg, code)
}

func requestLog(w httpserver.ResponseWriterPeeker, r *http.Request, start time.Time) {
	log.Printf("%s [%s] \"%s %s %s\" %d %d %dms",
		r.RemoteAddr, start.Format(time.RFC3339), r.Method, r.RequestURI,
		r.Proto, w.GetStatus(), w.GetContentLength(), time.Since(start).Milliseconds())
}

func readInt64Param(vals url.Values, key string) (int64, error) {
	if v := vals.Get(key); v == "" {
		return 0, fmt.Errorf("%q parameter is required", key)
	} else if i, err := strconv.ParseInt(v, 10, 64); err != nil {
		return 0, fmt.Errorf("invalid %q paramter", key)
	} else {
		return i, nil
	}
}
