package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"runtime"
	"strconv"
	"strings"
	"time"

	"cloud.google.com/go/compute/metadata"
	"github.com/ianrose14/solarsnoop/internal/notifications"
	"github.com/ianrose14/solarsnoop/internal/storage"
	"github.com/ianrose14/solarsnoop/pkg/enphase"
	"github.com/ianrose14/solarsnoop/pkg/httpserver"
)

func (svr *server) enphaseLoginHandler(rw http.ResponseWriter, r *http.Request) {
	w := httpserver.NewResponseWriterPeeker(rw)
	ctx := r.Context()

	start := time.Now()
	defer requestLog(w, r, start)

	authCode := r.URL.Query().Get("code")
	if authCode == "" {
		http.Error(w, "no code present in querystring", http.StatusBadRequest)
		return
	}

	authRsp, err := svr.enphaseClient.CompleteAuthorization(ctx, authCode, svr.enphaseCallbackUrl(r))
	if err != nil {
		httpError(w, err.Error(), nil, http.StatusInternalServerError)
		return
	}

	log.Printf("successful authorization for user %q", authRsp.UserId)

	systems, err := svr.enphaseClient.FetchSystems(ctx, authRsp.AccessToken, 1)
	if err != nil {
		httpError(w, err.Error(), nil, http.StatusInternalServerError)
		return
	}

	sessionToken, err := storage.UpsertSession(ctx, svr.db, authRsp.UserId, authRsp.AccessToken, authRsp.RefreshToken)
	if err != nil {
		httpError(w, "", err, http.StatusInternalServerError)
		return
	}

	log.Printf("created session %q for user %q", sessionToken, authRsp.UserId)

	err = storage.InsertSystems(ctx, svr.db, authRsp.UserId, systems)
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

	// TODO(ianrose): validate recipient based on kind

	if !hasSystemId(systems, systemId) {
		httpError(w, "invalid 'systemId' parameter", nil, http.StatusBadRequest)
		return
	}

	if kind == notifications.Ecobee {
		if recipient != "" {
			// assume this is a refresh token
			panic("TODO")
		}

		// initiate oauth flow
		qs := make(url.Values)
		qs.Set("response_type", "code")
		qs.Set("client_id", svr.secrets.Ecobee.ApiKey)
		qs.Set("redirect_uri", svr.ecobeeCallbackUrl(r))
		qs.Set("scope", "smartWrite")
		qs.Set("state", "systemId:"+strconv.FormatInt(systemId, 10))

		http.Redirect(w, r, "https://api.ecobee.com/authorize?"+qs.Encode(), http.StatusTemporaryRedirect)
		return
	}

	// for all non-Ecobee kinds, recipient is required
	if recipient == "" {
		httpError(w, "'recipient' is required", nil, http.StatusBadRequest)
		return
	}

	if err := storage.InsertNotifier(ctx, svr.db, session.UserId, systemId, kind, recipient); err != nil {
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

	if err := storage.DeleteNotifier(ctx, svr.db, session.UserId, systemId, notifierId); err != nil {
		httpError(w, fmt.Sprintf("failed to delete notifier %d", notifierId), err, http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
}

func (svr *server) ecobeeLoginHandler(rw http.ResponseWriter, r *http.Request) {
	w := httpserver.NewResponseWriterPeeker(rw)
	ctx := r.Context()

	start := time.Now()
	defer requestLog(w, r, start)

	authCode := r.URL.Query().Get("code")
	if authCode == "" {
		http.Error(w, "no code present in querystring", http.StatusBadRequest)
		return
	}

	const prefix = "systemId:"
	state := r.URL.Query().Get("state")
	if !strings.HasPrefix(state, prefix) {
		http.Error(w, "invalid 'state' param in querystring", http.StatusBadRequest)
		return
	}

	systemId, err := strconv.ParseInt(strings.TrimPrefix(state, prefix), 10, 64)
	if err != nil {
		httpError(w, "invalid 'state' param in querystring", err, http.StatusBadRequest)
		return
	}

	session, systems, err := getCurrentSession(ctx, r, svr.db)
	if err != nil {
		log.Printf("failed to lookup current session: %s", err)
	}
	if session == nil {
		httpError(w, "not logged in", nil, http.StatusUnauthorized)
		return
	}

	if !hasSystemId(systems, systemId) {
		httpError(w, "invalid 'systemId' from state parameter", nil, http.StatusBadRequest)
		return
	}

	authRsp, err := svr.ecobeeClient.CompleteAuthorization(ctx, authCode, svr.ecobeeCallbackUrl(r))
	if err != nil {
		httpError(w, err.Error(), nil, http.StatusInternalServerError)
		return
	}

	jsonStr, err := json.Marshal(authRsp)
	if err != nil {
		httpError(w, "failed to json-marshal auth response", err, http.StatusInternalServerError)
		return
	}

	if err := storage.InsertNotifier(ctx, svr.db, session.UserId, systemId, notifications.Ecobee, string(jsonStr)); err != nil {
		httpError(w, fmt.Sprintf("failed to save new config for system %d", systemId), err, http.StatusInternalServerError)
		return
	}

	message := fmt.Sprintf("Thermostat successfully connected to Enphase system %d 🌤", systemId)
	if err := svr.ecobeeClient.SendMessage(ctx, authRsp.AccessToken, message); err != nil {
		httpError(w, "failed to send message to ecobee device", err, http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
}

func (svr *server) ecobeeTestHandler(rw http.ResponseWriter, r *http.Request) {
	w := httpserver.NewResponseWriterPeeker(rw)
	ctx := r.Context()

	start := time.Now()
	defer requestLog(w, r, start)
	_ = ctx
}

func (svr *server) refreshHandler(rw http.ResponseWriter, r *http.Request) {
	w := httpserver.NewResponseWriterPeeker(rw)
	ctx := r.Context()

	start := time.Now()
	defer requestLog(w, r, start)

	if !strings.HasPrefix(svr.Host(r), "localhost") {
		http.Error(w, "admin handler not authorized", http.StatusUnauthorized)
		return
	}

	conn, err := svr.db.Conn(ctx)
	if err != nil {
		httpError(w, "", fmt.Errorf("failed to get database connection: %w", err), http.StatusInternalServerError)
		return
	}
	defer conn.Close()

	sessions, err := storage.QuerySessions(ctx, svr.db, "" /* all */)
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

		if _, err := storage.UpsertSession(ctx, svr.db, session.UserId, rsp.AccessToken, rsp.RefreshToken); err != nil {
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

	// We ONLY want to match on the exact / path.  Don't be a fallback!
	if r.URL.Path != "/" {
		log.Printf("not founding [%s]", r.URL.Path)
		http.NotFound(w, r)
		return
	}

	type NotifierConfig struct {
		storage.NotifierConfig
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
		OAuthClientId: svr.secrets.Enphase.ClientID,
		CallbackUrl:   svr.enphaseCallbackUrl(r),
	}

	log.Printf("CallbackUrl = %s", args.CallbackUrl)

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
		configs, err := storage.QuerySystemNotifiers(ctx, svr.db, session.UserId, system.SystemId)
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

	if !strings.HasPrefix(svr.Host(r), "localhost") || runtime.GOOS == "darwin" {
		http.Error(w, "admin handler not authorized", http.StatusUnauthorized)
		return
	}

	conn, err := svr.db.Conn(ctx)
	if err != nil {
		httpError(w, "", fmt.Errorf("failed to get database connection: %w", err), http.StatusInternalServerError)
		return
	}
	defer conn.Close()

	sessions, err := storage.QuerySessions(ctx, svr.db, "" /* all */)
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

func (svr *server) ecobeeCallbackUrl(r *http.Request) string {
	// NB: ecobee oauth flow only works over https
	return fmt.Sprintf("https://%s/ecobee/oauth/callback", svr.Host(r))
}

func (svr *server) enphaseCallbackUrl(r *http.Request) string {
	scheme := "https"
	if !metadata.OnGCE() && r.URL.Scheme != "" {
		scheme = r.URL.Scheme
	}
	return fmt.Sprintf("%s://%s/enphase/oauth/callback", scheme, svr.Host(r))
}

func getCurrentSession(ctx context.Context, r *http.Request, db *sql.DB) (*storage.AuthSession, []*enphase.System, error) {
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

	sessions, err := storage.QuerySessions(ctx, db, sessionToken)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to query sessions: %w", err)
	}

	if len(sessions) == 0 {
		log.Printf("no sessions found for this token")
		return nil, nil, nil
	}

	systems, err := storage.QuerySystems(ctx, db, sessions[0].UserId)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to query systems: %w", err)
	}

	return sessions[0], systems, nil
}

func hasSystemId(systems []*enphase.System, systemId int64) bool {
	for _, system := range systems {
		if system.SystemId == systemId {
			return true
		}
	}
	return false
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
