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
	"time"

	"github.com/ianrose14/solarsnoop/internal"
	"github.com/ianrose14/solarsnoop/internal/notifications"
	"github.com/ianrose14/solarsnoop/internal/powertrend"
	"github.com/ianrose14/solarsnoop/pkg/enphase"
	"github.com/ianrose14/solarsnoop/pkg/httpserver"
	_ "github.com/mattn/go-sqlite3"
)

// TODO: refresh tokens periodically

const (
	SessionCookieName   = "auth_session"
	SessionCookiePrefix = "v1:"
)

var (
	//go:embed data/enphase-apikey.txt
	enphaseApiKey string

	//go:embed data/enphase-clientid.txt
	enphaseClientId string

	//go:embed data/enphase-clientsecret.txt
	enphaseClientSecret string

	//go:embed data/sendgrid-apikey.txt
	sendgridApiKey string

	//go:embed templates/index.template
	rootContent string

	//go:embed favicon.ico static static/help
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

	if err := internal.UpsertDatabaseTables(ctx, db); err != nil {
		log.Fatalf("failed to upsert database tables: %s", err)
	}

	log.Printf("listening on port :%d", *port)

	svr := &server{
		db:            db,
		enphaseClient: enphase.NewClient(enphaseApiKey, enphaseClientId, enphaseClientSecret),
		notifier:      notifications.NewSender(sendgridApiKey),
	}

	http.HandleFunc("/", svr.rootHandler)
	http.HandleFunc("/logout", svr.logoutHandler)
	http.HandleFunc("/oauth/callback", svr.loginHandler)
	http.HandleFunc("/notifications/add", svr.addNotificationHandler)
	http.HandleFunc("/notifications/delete", svr.deleteNotificationHandler)
	http.Handle("/favicon.ico", http.FileServer(http.FS(staticContent)))
	http.Handle("/static/", http.FileServer(http.FS(staticContent)))

	// Admin handlers, remove or protect before real launch
	http.HandleFunc("/refresh", svr.refreshHandler)
	http.HandleFunc("/sessions", svr.sessionsHandler)
	http.HandleFunc("/tick", svr.cronTicker)

	err = http.ListenAndServe(fmt.Sprintf(":%d", *port), nil)
	log.Fatalf("ListenAndServe: %v", err)
}

type server struct {
	db            *sql.DB
	enphaseClient *enphase.Client
	notifier      *notifications.Sender
}

func (svr *server) cronTicker(rw http.ResponseWriter, r *http.Request) {
	w := httpserver.NewResponseWriterPeeker(rw)
	ctx := r.Context()

	start := time.Now()
	defer requestLog(w, r, start)

	sessions, err := internal.QuerySessions(ctx, svr.db, "" /* all */)
	if err != nil {
		httpError(w, err.Error(), nil, http.StatusInternalServerError)
		return
	}

	w.Header().Set("content-type", "text/plain")

	for _, session := range sessions {
		fmt.Fprintf(w, "processing session %s of user %s\n", session.SessionToken, session.UserId)

		systems, err := internal.QuerySystems(ctx, svr.db, session.UserId)
		if err != nil {
			log.Printf("failed to query systems for user %s", session.UserId)
			continue
		}

		fmt.Fprintf(w, "found %d systems for user %s\n", len(systems), session.UserId)
		for _, system := range systems {
			// We want to start at the beginning of the last FULL 15 minute interval,
			// but also it can (anecdotally) take up to 5 minutes for data to appear.
			// So our start should be so somewhere between 20 and 35 minutes ago.
			startAt := time.Now().Add(-5 * time.Minute).Truncate(15 * time.Minute).Add(-15 * time.Minute)
			endAt := startAt.Add(15 * time.Minute)

			wattsProduced, err := svr.enphaseClient.FetchProduction(ctx, system.SystemId, session.AccessToken, startAt)
			if err != nil {
				log.Printf("failed to query production for system %d of user %s: %s", system.SystemId, session.UserId, err)
				continue
			}
			fmt.Fprintf(w, "%d watts produced from %s to %s\n", wattsProduced, startAt, endAt)

			wattsConsumed, err := svr.enphaseClient.FetchConsumption(ctx, system.SystemId, session.AccessToken, startAt)
			if err != nil {
				log.Printf("failed to query consumption for system %d of user %s: %s", system.SystemId, session.UserId, err)
				continue
			}
			fmt.Fprintf(w, "%d watts consumed from %s to %s\n", wattsConsumed, startAt, endAt)

			err = internal.InsertUsageData(ctx, svr.db, session.UserId, system.SystemId, startAt, endAt, wattsProduced, wattsConsumed)
			if err != nil {
				log.Printf("failed to save telemetry for system %d of user %s: %s", system.SystemId, session.UserId, err)
				continue
			}

			phase, err := powertrend.CheckForStateTransitions()
			if err != nil {
				log.Printf("failed to check for state transitions for system %d of user %s: %s", system.SystemId, session.UserId, err)
				continue
			}

			if phase == powertrend.NoChange {
				log.Printf("no state transition for system %d of user %s: %s", system.SystemId, session.UserId, err)
				continue
			}

			notifs, err := internal.QueryNotifiers(ctx, svr.db, session.UserId, system.SystemId)
			if err != nil {
				log.Printf("failed to query configured notifications for system %d of user %s: %s", system.SystemId, session.UserId, err)
				continue
			}

			log.Printf("found %d configured notifications for system %d of user %s", len(notifs), system.SystemId, session.UserId)

			for _, notif := range notifs {
				sendErr := svr.notifier.Send(ctx, notifications.Kind(notif.Kind), notif.Recipient, phase)

				// always record the notification attempt, regardless of success/failure
				if err := internal.InsertMessageAttempt(ctx, svr.db, notif.Id, phase, sendErr); err != nil {
					log.Printf("failed to write notification attempt to database: %s", err)
				}

				if sendErr != nil {
					log.Printf("failed to send notification for system %d of user %s: %s", system.SystemId, session.UserId, sendErr)
					continue
				}
			}
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
