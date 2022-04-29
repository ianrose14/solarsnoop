package main

import (
	"context"
	"database/sql"
	"embed"
	_ "embed"
	"flag"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/ianrose14/solarsnoop/pkg/enphase"
	"github.com/ianrose14/solarsnoop/pkg/httpserver"
	_ "github.com/mattn/go-sqlite3"
)

const (
	SessionCookieName   = "auth_session"
	SessionCookiePrefix = "v1:"
)

var (
	//go:embed data/apikey.txt
	apiKey string

	//go:embed data/clientid.txt
	clientId string

	//go:embed data/clientsecret.txt
	clientSecret string

	//go:embed templates/index.template
	rootContent string

	//go:embed favicon.ico static
	staticContent embed.FS

	rootTemplate = template.Must(template.New("root").Parse(rootContent))
)

func main() {
	dbfile := flag.String("db", "solarsnoop.sqlite", "sqlite database file")
	port := flag.Int("port", 8080, "port to listen on")
	flag.Parse()

	ctx := context.Background()

	db, err := sql.Open("sqlite3", "file:"+*dbfile+"?cache=shared")
	if err != nil {
		log.Fatalf("failed to open sqlite connection: %s", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			log.Printf("warning: failed to cleanly close database: %s", err)
		}
	}()

	if err := upsertDatabaseTables(ctx, db); err != nil {
		log.Fatalf("failed to upsert database tables: %s", err)
	}

	log.Printf("listening on port :%d", *port)

	svr := &server{db: db}

	http.HandleFunc("/", svr.rootHandler)
	http.HandleFunc("/oauth/callback", svr.loginHandler)
	http.Handle("/favicon.ico", http.FileServer(http.FS(staticContent)))
	http.Handle("/static/", http.FileServer(http.FS(staticContent)))

	// Admin handlers, remove or protect before real launch
	http.HandleFunc("/sessions", svr.sessionsHandler)
	http.HandleFunc("/tick", svr.cronTicker)

	err = http.ListenAndServe(fmt.Sprintf(":%d", *port), nil)
	log.Fatalf("ListenAndServe: %v", err)
}

type server struct {
	db *sql.DB
}

func (svr *server) cronTicker(rw http.ResponseWriter, r *http.Request) {
	w := httpserver.NewResponseWriterPeeker(rw)
	ctx := r.Context()

	start := time.Now()
	defer requestLog(w, r, start)

	sessions, err := querySessions(ctx, svr.db, "" /* all */)
	if err != nil {
		httpError(w, err.Error(), nil, http.StatusInternalServerError)
		return
	}

	w.Header().Set("content-type", "text/plain")

	for _, session := range sessions {
		fmt.Fprintf(w, "processing session %s of user %s\n", session.SessionToken, session.UserId)

		systems, err := querySystems(ctx, svr.db, session.UserId)
		if err != nil {
			fmt.Fprintf(w, "failed to query systems for user %s\n", session.UserId)
			continue
		}

		fmt.Fprintf(w, "found %d systems for user %s\n", len(systems), session.UserId)
		for _, system := range systems {
			// We want to start at the beginning of the last FULL 15 minute interval,
			// but also it can (anecdotally) take up to 5 minutes for data to appear.
			// So our start should be so somewhere between 20 and 35 minutes ago.
			startAt := time.Now().Add(-5 * time.Minute).Truncate(15 * time.Minute).Add(-15 * time.Minute)

			wattsProduced, err := enphase.FetchProduction(ctx, system.SystemId, session.AccessToken, apiKey, startAt)
			if err != nil {
				fmt.Fprintf(w, "failed to query production for system %d of user %s: %s\n", system.SystemId, session.UserId, err)
				continue
			}
			fmt.Fprintf(w, "%d watts produced from %s to %s\n", wattsProduced, startAt, startAt.Add(15*time.Minute))

			wattsConsumed, err := enphase.FetchConsumption(ctx, system.SystemId, session.AccessToken, apiKey, startAt)
			if err != nil {
				fmt.Fprintf(w, "failed to query consumption for system %d of user %s: %s\n", system.SystemId, session.UserId, err)
				continue
			}
			fmt.Fprintf(w, "%d watts consumed from %s to %s\n", wattsConsumed, startAt, startAt.Add(15*time.Minute))

		}
	}

	/*
			poll:
		  - for all (active?) accounts
		    - store current net production
		    - if net production over past X minutes is [TBD] AND
		      - if last notification time is not too recent then
		        - send notification
		        - record last notification time
	*/
}

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

	authRsp, err := enphase.CompleteAuthorization(ctx, authCode, fmt.Sprintf("//%s/oauth/callback", r.Host), clientId, clientSecret)
	if err != nil {
		httpError(w, err.Error(), nil, http.StatusInternalServerError)
		return
	}

	log.Printf("successful authorization for user %q", authRsp.UserId)

	systems, err := enphase.FetchSystems(ctx, authRsp.AccessToken, apiKey)
	if err != nil {
		httpError(w, err.Error(), nil, http.StatusInternalServerError)
		return
	}

	session, err := insertSession(ctx, svr.db, authRsp.UserId, authRsp.AccessToken, authRsp.RefreshToken)
	if err != nil {
		httpError(w, "", err, http.StatusInternalServerError)
		return
	}

	log.Printf("created session %q for user %q", session.SessionToken, session.UserId)

	err = insertSystems(ctx, svr.db, session.UserId, systems)
	if err != nil {
		httpError(w, "", err, http.StatusInternalServerError)
		return
	}

	log.Printf("saved %d enphase systems to db", len(systems))

	// TODO - secure=true, samesite=???
	http.SetCookie(w, &http.Cookie{
		Name:    SessionCookieName,
		Value:   SessionCookiePrefix + session.SessionToken,
		Path:    "/",
		Expires: time.Now().Add(365 * 24 * time.Hour),
	})

	fmt.Fprintf(w, "done!!") // TODO
}

func (svr *server) rootHandler(rw http.ResponseWriter, r *http.Request) {
	w := httpserver.NewResponseWriterPeeker(rw)
	ctx := r.Context()

	start := time.Now()
	defer requestLog(w, r, start)

	args := struct {
		UserId        string
		Systems       []*enphase.System
		OAuthClientId string
		CallbackUrl   string
	}{
		OAuthClientId: clientId,
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

	sessions, err := querySessions(ctx, svr.db, "" /* all */)
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

func getCurrentSession(ctx context.Context, r *http.Request, db *sql.DB) (*authSession, []*enphase.System, error) {
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

	sessions, err := querySessions(ctx, db, sessionToken)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to query sessions: %w", err)
	}

	if len(sessions) == 0 {
		log.Printf("no sessions found for this token")
		return nil, nil, nil
	}

	systems, err := querySystems(ctx, db, sessions[0].UserId)
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
