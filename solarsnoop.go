package main

import (
	"context"
	"database/sql"
	"embed"
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"reflect"
	"runtime"
	"time"

	"github.com/ianrose14/solarsnoop/internal"
	"github.com/ianrose14/solarsnoop/internal/notifications"
	"github.com/ianrose14/solarsnoop/internal/powertrend"
	"github.com/ianrose14/solarsnoop/internal/storage"
	"github.com/ianrose14/solarsnoop/pkg/ecobee"
	"github.com/ianrose14/solarsnoop/pkg/enphase"
	"github.com/ianrose14/solarsnoop/pkg/httpserver"
	_ "github.com/mattn/go-sqlite3"
	"github.com/robfig/cron/v3"
	"golang.org/x/crypto/acme/autocert"
)

// TODO: refresh tokens periodically

const (
	SessionCookieName   = "auth_session"
	SessionCookiePrefix = "v1:"
)

var (
	//go:embed templates/index.template
	rootContent string

	//go:embed favicon.ico static static/help
	staticContent embed.FS

	rootTemplate = template.Must(template.New("root").Parse(rootContent))
)

func init() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds | log.Llongfile)
}

func main() {
	certsDir := flag.String("certs", "certs", "directory to store letsencrypt certs")
	dbfile := flag.String("db", "solarsnoop.sqlite", "sqlite database file")
	host := flag.String("host", "", "optional hostname for webserver")
	secretsFile := flag.String("secrets", "config/secrets.yaml", "Path to local secrets file")
	flag.Parse()

	ctx := context.Background()

	secrets, err := internal.ParseSecrets(*secretsFile)
	if err != nil {
		log.Fatalf("failed to parse secrets: %s", err)
	}

	db, err := sql.Open("sqlite3", "file:"+*dbfile+"?cache=shared")
	if err != nil {
		log.Fatalf("failed to open sqlite connection: %s", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			log.Printf("warning: failed to cleanly close database: %s", err)
		}
	}()

	if err := storage.UpsertDatabaseTables(ctx, db); err != nil {
		log.Fatalf("failed to upsert database tables: %s", err)
	}

	svr := &server{
		db:            db,
		ecobeeClient:  ecobee.NewClient(secrets.Ecobee.ApiKey),
		enphaseClient: enphase.NewClient(secrets.Enphase.ApiKey, secrets.Enphase.ClientID, secrets.Enphase.ClientSecret),
		secrets:       secrets,
		notifier:      notifications.NewSender(secrets.SendGrid.ApiKey),
		host:          *host,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", svr.rootHandler)
	mux.HandleFunc("/logout", svr.logoutHandler)
	mux.HandleFunc("/ecobee/oauth/callback", svr.ecobeeLoginHandler)
	mux.HandleFunc("/enphase/oauth/callback", svr.enphaseLoginHandler)
	mux.HandleFunc("/notifications/add", svr.addNotificationHandler)
	mux.HandleFunc("/notifications/delete", svr.deleteNotificationHandler)
	mux.Handle("/favicon.ico", http.FileServer(http.FS(staticContent)))
	mux.Handle("/static/", http.FileServer(http.FS(staticContent)))

	// NOSUBMIT:
	svr.refreshEnphaseTokens(ctx)
	svr.refreshEcobeeTokens(ctx)
	svr.refreshThermostats(ctx)

	// TODO: 1 minute ticker to refresh all enphase and ecobee tokens that are < 15min from expiration?
	wrap := func(f func(context.Context) error) func() {
		return func() {
			if err := f(ctx); err != nil {
				log.Printf("error in %s: %s", getFunctionName(f), err)
			}
		}
	}
	c := cron.New()
	if _, err := c.AddFunc("*/15 * * * *", wrap(svr.refreshEnphaseTokens)); err != nil {
		log.Fatalf("bad cronspec: %s", err)
	}
	if _, err := c.AddFunc("*/15 * * * *", wrap(svr.refreshEcobeeTokens)); err != nil {
		log.Fatalf("bad cronspec: %s", err)
	}
	// TODO(ianrose): more frequent:
	if _, err := c.AddFunc("0 12 * * *", wrap(svr.checkForUpdates)); err != nil {
		log.Fatalf("bad cronspec: %s", err)
	}
	if _, err := c.AddFunc("0 12 * * *", wrap(svr.refreshThermostats)); err != nil {
		log.Fatalf("bad cronspec: %s", err)
	}
	c.Start()

	defer c.Stop()

	// Admin handlers, remove or protect before real launch
	mux.HandleFunc("/refresh", svr.refreshHandler)
	mux.HandleFunc("/sessions", svr.sessionsHandler)
	mux.HandleFunc("/tick", svr.cronTicker)
	mux.HandleFunc("/ecobee", svr.ecobeeTestHandler)

	var httpHandler http.Handler = mux

	// TODO: in a handler wrapper, redirect http to https (in production only)

	const inDev = runtime.GOOS == "darwin"

	// when testing locally it doesn't make sense to start
	// HTTPS server, so only do it in production.
	// In real code, I control this with -production cmd-line flag
	if !inDev {
		if err := os.MkdirAll(*certsDir, 0777); err != nil {
			log.Fatalf("failed to create certs dir: %s", err)
		}

		httpsSrv := makeHTTPServer(mux)
		certManager := &autocert.Manager{
			Prompt: autocert.AcceptTOS,
			Email:  "ianrose14+autocert@gmail.com", // NOSUBMIT - replace with alias?
			HostPolicy: func(ctx context.Context, host string) error {
				log.Printf("autocert query for host %q", host)
				return autocert.HostWhitelist("solarsnoop.com", "www.solarsnoop.com")(ctx, host)
			},
			Cache: autocert.DirCache(*certsDir),
		}
		httpsSrv.Addr = ":https"
		httpsSrv.TLSConfig = certManager.TLSConfig()

		httpHandler = certManager.HTTPHandler(mux)

		go func() {
			log.Printf("listening on port 443")

			err := httpsSrv.ListenAndServeTLS("", "")
			if err != nil {
				log.Fatalf("httpsSrv.ListendAndServeTLS() failed: %s", err)
			}
		}()
	}

	log.Printf("listening on port 80")
	if err := http.ListenAndServe(":http", httpHandler); err != nil {
		log.Fatalf("http.ListenAndServe failed: %s", err)
	}
}

func makeHTTPServer(mux *http.ServeMux) *http.Server {
	// set timeouts so that a slow or malicious client can't hold resources forever
	return &http.Server{
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
		IdleTimeout:  120 * time.Second,
		Handler:      mux,
	}
}

type server struct {
	db            *sql.DB
	ecobeeClient  *ecobee.Client
	enphaseClient *enphase.Client
	secrets       *internal.SecretsFile
	notifier      *notifications.Sender
	host          string
}

func (svr *server) refreshEcobeeTokens(ctx context.Context) error {
	conn, err := svr.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get database connection: %w", err)
	}
	defer conn.Close()

	accounts, err := storage.QueryEcobeeAccounts(ctx, svr.db)
	if err != nil {
		return fmt.Errorf("failed to query for ecobee accounts: %w", err)
	}

	var updated int
	for _, account := range accounts {
		newRsp, err := svr.ecobeeClient.RefreshTokens(ctx, account.RefreshToken)
		if err != nil {
			log.Printf("failed to refresh ecobee tokens for enphase system %d: %s", account.EnphaseSystemId, err)
			continue
		}

		if err := storage.UpdateEcobeeAccount(ctx, svr.db, account.UserId, account.EnphaseSystemId, newRsp.AccessToken); err != nil {
			log.Printf("failed to update db for enphase system %d %d: %s", account.EnphaseSystemId, err)
			continue
		}

		updated++
	}

	log.Printf("refreshEcobeeTokens complete: refreshed %d tokens", updated)
	return nil
}

func (svr *server) refreshEnphaseTokens(ctx context.Context) error {
	conn, err := svr.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get database connection: %w", err)
	}
	defer conn.Close()

	sessions, err := storage.QuerySessions(ctx, svr.db, "" /* all */)
	if err != nil {
		return fmt.Errorf("failed to query sessions: %w", err)
	}

	var updated int
	for _, session := range sessions {
		rsp, err := svr.enphaseClient.RefreshTokens(ctx, session.RefreshToken)
		if err != nil {
			log.Printf("failed to refresh enphase tokens for session for user %s: %s", session.UserId, err)
			continue
		}

		if _, err := storage.UpsertSession(ctx, svr.db, session.UserId, rsp.AccessToken, rsp.RefreshToken); err != nil {
			log.Printf("failed to upsert session for user %s: %s", session.UserId, err)
			continue
		}

		updated++
	}

	log.Printf("refreshEcobeeTokens complete: refreshed %d tokens", updated)
	return nil
}

func (svr *server) refreshThermostats(ctx context.Context) error {
	conn, err := svr.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get database connection: %w", err)
	}
	defer conn.Close()

	notifiers, err := storage.QueryAllNotifiers(ctx, svr.db)
	if err != nil {
		return fmt.Errorf("failed to query for notifiers: %w", err)
	}

	var updated int
	for _, notifier := range notifiers {
		if notifier.Kind != notifications.Ecobee {
			continue
		}

		var authRsp ecobee.OAuthTokenResponse
		if err := json.Unmarshal([]byte(notifier.Recipient), &authRsp); err != nil {
			log.Printf("failed to json-unmarshal Recipient into auth response for notifier %d: %s", notifier.Id, err)
			continue
		}

		thermostats, err := svr.ecobeeClient.FetchThermostats(authRsp.AccessToken)
		if err != nil {
			log.Printf("failed to fetch thermostats for notifier %d: %s", notifier.Id, err)
			continue
		}

		for _, thermostat := range thermostats {
			log.Printf("thermostat %s (%s)", thermostat.Identifier, thermostat.Name)
			log.Printf("UTCTime = %s", thermostat.UTCTs)
			log.Printf("ThermostatTime = %s", thermostat.ThermostatTs)
			log.Printf("Weather = %v", thermostat.Weather)
			log.Printf("lastmodified = %s", thermostat.LastModifiedTs)

			tzdiff := thermostat.ThermostatTs.Sub(thermostat.UTCTs)
			tzOffset := tzdiff / time.Minute

			log.Printf("tzoffset (minutes) = %d", tzOffset)
		}

		updated++
	}

	log.Printf("refreshThermostats complete: refreshed %d ecobees", updated)
	return nil
}

func (svr *server) checkForUpdates(ctx context.Context) error {
	sessions, err := storage.QuerySessions(ctx, svr.db, "" /* all */)
	if err != nil {
		return fmt.Errorf("failed to query sessions: %w", err)
	}

	for _, session := range sessions {
		log.Printf("processing session %s of user %s", session.SessionToken, session.UserId)

		systems, err := storage.QuerySystems(ctx, svr.db, session.UserId)
		if err != nil {
			log.Printf("failed to query systems for user %s", session.UserId)
			continue
		}

		log.Printf("found %d systems for user %s", len(systems), session.UserId)
		for _, system := range systems {
			calculateDesiredAcount

			/*
			 * On each tick:
			 * 1. calculate desired action (HOLD, PRODUCE or CONSUME) and "reason" (text)
			 *   - this can probably be 100% based on the enphase systems timezone + recent usage/production
			 * 2. apply desired action to each notifier (rename to actor? or actuator?)
			 * 3. this results in a executed action (again, HOLD, PRODUCE or CONSUME) and "reason" (text)
			 * 4. the realized action can either succeed or fail
			 * 5. all of these are stored in a log: desired_action, desired_action_reason, executed_action, executed_action_reason, result
			 *
			 * example 1: desired = CONSUME due to overproduction, executed = HOLD due to vacation event on thermostat, result = success (trivially)
			 * example 2: desired = HOLD due to night-time, executed = HOLD (no reason), result = success (trivially)
			 * example 3: desired = CONSUME due to overproduction, executed = CONSUME due to thermostat ready, result = fail (http error)
			 * example 4: desired = PRODUCE due to overconsumption, executed = HOLD due to change to thermostat state, result = success (trivially)
			 */

			// If it is morning/evening/night for this system, skip it to save API quota.

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
			log.Printf("%d watts produced from %s to %s", wattsProduced, startAt, endAt)

			wattsConsumed, err := svr.enphaseClient.FetchConsumption(ctx, system.SystemId, session.AccessToken, startAt)
			if err != nil {
				log.Printf("failed to query consumption for system %d of user %s: %s", system.SystemId, session.UserId, err)
				continue
			}
			log.Printf("%d watts consumed from %s to %s", wattsConsumed, startAt, endAt)

			err = storage.InsertUsageData(ctx, svr.db, session.UserId, system.SystemId, startAt, endAt, wattsProduced, wattsConsumed)
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

			notifs, err := storage.QuerySystemNotifiers(ctx, svr.db, session.UserId, system.SystemId)
			if err != nil {
				log.Printf("failed to query configured notifications for system %d of user %s: %s", system.SystemId, session.UserId, err)
				continue
			}

			log.Printf("found %d configured notifications for system %d of user %s", len(notifs), system.SystemId, session.UserId)

			for _, notif := range notifs {
				sendErr := svr.notifier.Send(ctx, notif.Kind, notif.Recipient, phase)

				// always record the notification attempt, regardless of success/failure
				if err := storage.InsertMessageAttempt(ctx, svr.db, notif.Id, phase, sendErr); err != nil {
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
	return nil
}

func (svr *server) cronTicker(rw http.ResponseWriter, r *http.Request) {
	w := httpserver.NewResponseWriterPeeker(rw)
	ctx := r.Context()

	start := time.Now()
	defer requestLog(w, r, start)

	sessions, err := storage.QuerySessions(ctx, svr.db, "" /* all */)
	if err != nil {
		httpError(w, err.Error(), nil, http.StatusInternalServerError)
		return
	}

	w.Header().Set("content-type", "text/plain")

	for _, session := range sessions {
		fmt.Fprintf(w, "processing session %s of user %s\n", session.SessionToken, session.UserId)

		systems, err := storage.QuerySystems(ctx, svr.db, session.UserId)
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

			err = storage.InsertUsageData(ctx, svr.db, session.UserId, system.SystemId, startAt, endAt, wattsProduced, wattsConsumed)
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

			notifs, err := storage.QuerySystemNotifiers(ctx, svr.db, session.UserId, system.SystemId)
			if err != nil {
				log.Printf("failed to query configured notifications for system %d of user %s: %s", system.SystemId, session.UserId, err)
				continue
			}

			log.Printf("found %d configured notifications for system %d of user %s", len(notifs), system.SystemId, session.UserId)

			for _, notif := range notifs {
				sendErr := svr.notifier.Send(ctx, notif.Kind, notif.Recipient, phase)

				// always record the notification attempt, regardless of success/failure
				if err := storage.InsertMessageAttempt(ctx, svr.db, notif.Id, phase, sendErr); err != nil {
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

func (svr *server) Host(r *http.Request) string {
	if svr.host != "" {
		return svr.host
	}
	return r.Host
}

func getFunctionName(i interface{}) string {
	return runtime.FuncForPC(reflect.ValueOf(i).Pointer()).Name()
}
