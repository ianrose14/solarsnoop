package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"runtime"
	"strconv"
	"strings"
	"time"

	"cloud.google.com/go/compute/metadata"
	"github.com/ianrose14/solarsnoop/internal/powersinks"
	"github.com/ianrose14/solarsnoop/internal/storage"
	"github.com/ianrose14/solarsnoop/pkg/ecobee"
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

	for _, system := range systems {
		params := storage.InsertEnphaseSystemParams{
			UserID:     authRsp.UserId,
			SystemID:   system.SystemId,
			Name:       system.Name,
			PublicName: system.PublicName,
		}
		err := storage.New(svr.db).InsertEnphaseSystem(ctx, params)
		if err != nil {
			httpError(w, "", err, http.StatusInternalServerError)
			return
		}
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

func (svr *server) addPowersinkHandler(rw http.ResponseWriter, r *http.Request) {
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

	channel := powersinks.Channel(strings.ToLower(r.Form.Get("kind")))
	if channel == "" {
		httpError(w, "'kind' is required", nil, http.StatusBadRequest)
		return
	}
	if !channel.IsValid() {
		httpError(w, "invalid 'kind' parameter", nil, http.StatusBadRequest)
		return
	}

	recipient := r.Form.Get("recipient")

	// TODO(ianrose): validate recipient based on channel

	if !hasSystemId(systems, systemId) {
		httpError(w, "invalid 'systemId' parameter", nil, http.StatusBadRequest)
		return
	}

	// Ecobee is a special case...
	if channel == powersinks.Ecobee {
		if recipient == "" {
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

		newRsp, err := svr.ecobeeClient.RefreshTokens(ctx, recipient)
		if err != nil {
			httpError(w, "failed to refresh ecobee tokens", err, http.StatusInternalServerError)
			return
		}

		if err := svr.saveEcobeeData(ctx, newRsp, session, systemId); err != nil {
			httpError(w, err.Error(), nil, http.StatusInternalServerError)
			return
		}
	} else {
		if channel.RequiresRecipient() && recipient == "" {
			httpError(w, "'recipient' is required", nil, http.StatusBadRequest)
			return
		}

		params := storage.InsertPowersinkParams{
			UserID:    session.UserID,
			SystemID:  systemId,
			Created:   time.Now(),
			Channel:   string(channel),
			Recipient: storage.Str(recipient),
		}
		if err := storage.New(svr.db).InsertPowersink(ctx, params); err != nil {
			httpError(w, fmt.Sprintf("failed to save new config for system %d", systemId), err, http.StatusInternalServerError)
			return
		}
	}

	http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
}

func (svr *server) deletePowersinkHandler(rw http.ResponseWriter, r *http.Request) {
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

	powersinkId, err := readInt64Param(r.Form, "powersinkId")
	if err != nil {
		httpError(w, err.Error(), nil, http.StatusBadRequest) // this errMsg is always safe to echo to client
		return
	}

	systemId, err := readInt64Param(r.Form, "systemId")
	if err != nil {
		httpError(w, err.Error(), nil, http.StatusBadRequest) // this errMsg is always safe to echo to client
		return
	}

	params := storage.DeletePowersinkParams{
		UserID:      session.UserID,
		SystemID:    systemId,
		PowersinkID: int32(powersinkId),
	}
	if err := storage.New(svr.db).DeletePowersink(ctx, params); err != nil {
		httpError(w, fmt.Sprintf("failed to delete powersink %d", powersinkId), err, http.StatusInternalServerError)
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

	if err := svr.saveEcobeeData(ctx, authRsp, session, systemId); err != nil {
		httpError(w, err.Error(), nil, http.StatusInternalServerError)
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

	sessions, err := storage.New(svr.db).QuerySessionsAll(ctx)
	if err != nil {
		httpError(w, "", fmt.Errorf("failed to query sessions: %w", err), http.StatusInternalServerError)
		return
	}

	for _, session := range sessions {
		rsp, err := svr.enphaseClient.RefreshTokens(ctx, session.RefreshToken)
		if err != nil {
			log.Printf("failed to refresh tokens for session for user %s: %s", session.UserID, err)
			continue
		}

		if _, err := storage.UpsertSession(ctx, svr.db, session.UserID, rsp.AccessToken, rsp.RefreshToken); err != nil {
			log.Printf("failed to upsert session for user %s: %s", session.UserID, err)
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

	type PowersinkConfig struct {
		storage.QueryPowersinksForSystemRow
		SystemId   int64
		SystemName string
	}

	args := struct {
		UserId        string
		Systems       []*enphase.System
		Powersinks    []PowersinkConfig
		OAuthClientId string
		CallbackUrl   string
	}{
		OAuthClientId: svr.secrets.Enphase.ClientID,
		CallbackUrl:   svr.enphaseCallbackUrl(r),
	}

	session, systems, err := getCurrentSession(ctx, r, svr.db)
	if err != nil {
		log.Printf("failed to lookup current session: %s", err)
	}
	if session != nil {
		args.UserId = session.UserID
		args.Systems = systems
		log.Printf("found %d systems for user %s", len(systems), session.UserID)
	}

	for _, system := range systems {
		params := storage.QueryPowersinksForSystemParams{
			UserID:   session.UserID,
			SystemID: system.SystemId,
		}
		rows, err := storage.New(svr.db).QueryPowersinksForSystem(ctx, params)
		if err != nil {
			httpError(w, fmt.Sprintf("failed to query configured powersinks for system %d", system.SystemId), err, http.StatusInternalServerError)
			return
		}
		for _, row := range rows {
			args.Powersinks = append(args.Powersinks, PowersinkConfig{
				QueryPowersinksForSystemRow: row,
				SystemId:                    system.SystemId,
				SystemName:                  system.Name,
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

	sessions, err := storage.New(svr.db).QuerySessionsAll(ctx)
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

func (svr *server) saveEcobeeData(ctx context.Context, authRsp *ecobee.OAuthTokenResponse, session *storage.AuthSession, systemId int64) error {
	thermostats, err := svr.ecobeeClient.FetchThermostats(authRsp.AccessToken)
	if err != nil {
		return fmt.Errorf("failed to fetch thermostats: %w", err)
	}

	acctParams := storage.InsertEcobeeAccountParams{
		UserID:          session.UserID,
		EnphaseSystemID: systemId,
		AccessToken:     authRsp.AccessToken,
		RefreshToken:    authRsp.RefreshToken,
		CreatedTime:     time.Now(),
		LastRefreshTime: time.Now(),
	}
	err = storage.New(svr.db).InsertEcobeeAccount(ctx, acctParams)
	if err != nil {
		return fmt.Errorf("failed to save ecobee account: %w", err)
	}

	for _, thermostat := range thermostats {
		params := storage.InsertEcobeeThermostatParams{
			UserID:          session.UserID,
			EnphaseSystemID: systemId,
			ThermostatID:    thermostat.Identifier,
		}
		err := storage.New(svr.db).InsertEcobeeThermostat(ctx, params)
		if err != nil {
			return fmt.Errorf("failed to save ecobee thermostat %q: %w", thermostat.Identifier, err)
		}
	}

	powersinkParams := storage.InsertPowersinkParams{
		UserID:    session.UserID,
		SystemID:  systemId,
		Created:   time.Now(),
		Channel:   string(powersinks.Ecobee),
		Recipient: sql.NullString{},
	}
	if err := storage.New(svr.db).InsertPowersink(ctx, powersinkParams); err != nil {
		return fmt.Errorf("failed to save new config for system %d: %w", systemId, err)
	}

	message := fmt.Sprintf("Thermostat successfully connected to Enphase system %d", systemId)
	if err := svr.ecobeeClient.SendMessage(ctx, authRsp.AccessToken, message); err != nil {
		return fmt.Errorf("failed to send message to ecobee device: %w", err)
	}

	return nil
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

	sessions, err := storage.New(db).QuerySessions(ctx, sessionToken)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to query sessions: %w", err)
	}

	if len(sessions) == 0 {
		log.Printf("no sessions found for this token")
		return nil, nil, nil
	}

	rows, err := storage.New(db).QueryEnphaseSystems(ctx, sessions[0].UserID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to query systems: %w", err)
	}

	var systems []*enphase.System
	for _, row := range rows {
		systems = append(systems, &enphase.System{
			SystemId:   row.SystemID,
			Name:       row.Name,
			PublicName: row.PublicName,
			Timezone:   time.UTC.String(), // NOSUBMIT make this real
		})
	}

	return &sessions[0], systems, nil
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
