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
	"github.com/ianrose14/solarsnoop/internal/powersinks"
	"github.com/ianrose14/solarsnoop/internal/storage"
	"github.com/ianrose14/solarsnoop/pkg/ecobee"
	"github.com/ianrose14/solarsnoop/pkg/enphase"
	_ "github.com/mattn/go-sqlite3"
	"github.com/robfig/cron/v3"
	"golang.org/x/crypto/acme/autocert"
)

const (
	SessionCookieName   = "auth_session"
	SessionCookiePrefix = "v1:"
)

var (
	//go:embed templates/index.template
	rootContent string

	//go:embed favicon.ico static static/help
	staticContent embed.FS

	//go:embed config/schema.sql
	dbSchema string

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

	if err := storage.UpsertDatabaseTables(ctx, db, dbSchema); err != nil {
		log.Fatalf("failed to upsert database tables: %s", err)
	}

	svr := &server{
		db:            db,
		ecobeeClient:  ecobee.NewClient(secrets.Ecobee.ApiKey),
		enphaseClient: enphase.NewClient(secrets.Enphase.ApiKey, secrets.Enphase.ClientID, secrets.Enphase.ClientSecret),
		secrets:       secrets,
		notifier:      powersinks.NewExtApi(*host, secrets.SendGrid.ApiKey),
		host:          *host,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", svr.rootHandler)
	mux.HandleFunc("/logout", svr.logoutHandler)
	mux.HandleFunc("/ecobee/oauth/callback", svr.ecobeeLoginHandler)
	mux.HandleFunc("/enphase/oauth/callback", svr.enphaseLoginHandler)
	mux.HandleFunc("/powersinks/add", svr.addPowersinkHandler)
	mux.HandleFunc("/powersinks/delete", svr.deletePowersinkHandler)
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
	notifier      *powersinks.ExtApi
	host          string
}

func (svr *server) refreshEcobeeTokens(ctx context.Context) error {
	conn, err := svr.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get database connection: %w", err)
	}
	defer conn.Close()

	accounts, err := storage.New(svr.db).QueryEcobeeAccounts(ctx)
	if err != nil {
		return fmt.Errorf("failed to query for ecobee accounts: %w", err)
	}

	var updated int
	for _, account := range accounts {
		newRsp, err := svr.ecobeeClient.RefreshTokens(ctx, account.RefreshToken)
		if err != nil {
			log.Printf("failed to refresh ecobee tokens for enphase system %d: %s", account.EnphaseSystemID, err)
			continue
		}

		params := storage.UpdateEcobeeAccessTokenParams{
			AccessToken:     newRsp.AccessToken,
			UserID:          account.UserID,
			EnphaseSystemID: account.EnphaseSystemID,
		}
		if err := storage.New(svr.db).UpdateEcobeeAccessToken(ctx, params); err != nil {
			log.Printf("failed to update db for enphase system %d: %s", account.EnphaseSystemID, err)
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

	sessions, err := storage.New(svr.db).QuerySessionsAll(ctx)
	if err != nil {
		return fmt.Errorf("failed to query sessions: %w", err)
	}

	var updated int
	for _, session := range sessions {
		rsp, err := svr.enphaseClient.RefreshTokens(ctx, session.RefreshToken)
		if err != nil {
			log.Printf("failed to refresh enphase tokens for session for user %s: %s", session.UserID, err)
			continue
		}

		if _, err := storage.UpsertSession(ctx, svr.db, session.UserID, rsp.AccessToken, rsp.RefreshToken); err != nil {
			log.Printf("failed to upsert session for user %s: %s", session.UserID, err)
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

	sinks, err := storage.New(svr.db).QueryPowersinksAll(ctx)
	if err != nil {
		return fmt.Errorf("failed to query for powersinks: %w", err)
	}

	var updated int
	for _, powersink := range sinks {
		if powersinks.Channel(powersink.Channel) != powersinks.Ecobee {
			continue
		}

		var authRsp ecobee.OAuthTokenResponse
		if err := json.Unmarshal([]byte(powersink.Recipient.String), &authRsp); err != nil {
			log.Printf("failed to json-unmarshal Recipient into auth response for powersink %d: %s", powersink.PowersinkID, err)
			continue
		}

		thermostats, err := svr.ecobeeClient.FetchThermostats(authRsp.AccessToken)
		if err != nil {
			log.Printf("failed to fetch thermostats for powersink %d: %s", powersink.PowersinkID, err)
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
	sessions, err := storage.New(svr.db).QuerySessionsAll(ctx)
	if err != nil {
		return fmt.Errorf("failed to query sessions: %w", err)
	}

	for _, session := range sessions {
		log.Printf("processing session %s of user %s", session.SessionToken, session.UserID)

		systems, err := storage.New(svr.db).QueryEnphaseSystems(ctx, session.UserID)
		if err != nil {
			log.Printf("failed to query systems for user %s", session.UserID)
			continue
		}

		log.Printf("found %d systems for user %s", len(systems), session.UserID)
		for _, system := range systems {
			queryParams := storage.QueryPowersinksForSystemParams{
				UserID:   session.UserID,
				SystemID: system.SystemID,
			}
			sinks, err := storage.New(svr.db).QueryPowersinksForSystem(ctx, queryParams)
			if err != nil {
				log.Printf("failed to query configured powersinks for system %d of user %s: %s", system.SystemID, session.UserID, err)
				continue
			}

			log.Printf("found %d configured powersinks for system %d of user %s", len(sinks), system.SystemID, session.UserID)
			factory := powersinks.NewFactory(svr.enphaseClient, &system, &session, svr.notifier)

			for _, sink := range sinks {
				actions, err := storage.New(svr.db).FetchRecentActions(ctx, sink.PowersinkID)
				if err != nil {
					log.Printf("failed to query recent actions from DB for powersink %d: %+v", sink.PowersinkID, err)
					continue
				}

				result, err := factory.Execute(ctx, sink, actions)
				if err != nil {
					log.Printf("failed to execute powersink %d: %+v", sink.PowersinkID, err)
					continue
				}
				params := storage.RecordActionParams{
					PowersinkID:    sink.PowersinkID,
					Timestamp:      time.Now(),
					DesiredAction:  string(result.Desired),
					DesiredReason:  storage.Str(result.DesiredReason),
					ExecutedAction: string(result.Executed),
					ExecutedReason: storage.Str(result.ExecutedReason),
					Success:        result.Success,
					SuccessReason:  storage.Str(result.SuccessReason),
				}
				if err := storage.New(svr.db).RecordAction(ctx, params); err != nil {
					log.Printf("failed to record action to database for powersink %d: %+v", sink.PowersinkID, err)
				}
			}

			// calculateDesiredAcount

			/*
			 * On each tick:
			 * 1. query for all enabled powersinks
			 * 2. for each powersink, check which commands are currently acceptable.  If the only one is NONE,
			 *    then skip step 3.  examples:
			 *    - ecobees in vacation mode
			 *    - sms/email if last command was PRODUCE and it was "recent" (we don't want to spam people)
			 * 3. calculate desired action (NONE, PRODUCE or CONSUME) and "reason" (text)
			 *   - this can probably be 100% based on the enphase system's timezone + recent usage/production
			 * 4. apply desired action to each powersink
			 * 5. this results in an executed action (again, NONE, PRODUCE or CONSUME) and "reason" (text)
			 * 6. the realized action can either succeed or fail
			 * 7. all of these are stored in a log: powersink_id, desired_action, desired_action_reason, executed_action, executed_action_reason, result
			 *
			 * ecobee examples:
			 * example 1: desired = CONSUME due to overproduction, executed = NONE due to vacation event on thermostat, result = success (trivially)
			 * example 2: desired = PRODUCE due to night-time, executed = PRODUCE (last command still in effect), result = success
			 * example 2b: desired = PRODUCE due to night-time, executed = NONE (last command not in effect), result = success (trivially)
			 * example 3: desired = CONSUME due to overproduction, executed = CONSUME due to thermostat ready, result = fail (http error)
			 * example 4: desired = PRODUCE due to overconsumption, executed = NONE due to change to thermostat state, result = success (trivially)
			 * example 5: desired = NONE due to balanced production, executed = NONE (no reason), result = success (trivially)
			 *
			 * SMS/email examples:
			 * example 1: desired = CONSUME due to overproduction, executed = CONSUME (no reason), result = success
			 * example 2: desired = CONSUME due to overproduction, executed = CONSUME (no reason), result = fail (http error)
			 * example 3: desired = PRODUCE due to night-time, executed = PRODUCE (last command recent), result = success
			 * example 4: desired = PRODUCE due to night-time, executed = NONE (last command not recent), result = success (trivially)
			 * example 5: desired = PRODUCE due to overconsumption, executed = PRODUCE (no reason), result = success
			 * example 6: desired = NONE due to balanced production, executed = NONE (no reason), result = success (trivially)
			 *
			 */

			// If it is morning/evening/night for this system, skip it to save API quota.

			// We want to start at the beginning of the last FULL 15 minute interval,
			// but also it can (anecdotally) take up to 5 minutes for data to appear.
			// So our start should be so somewhere between 20 and 35 minutes ago.
			startAt := time.Now().Add(-5 * time.Minute).Truncate(15 * time.Minute).Add(-15 * time.Minute)
			endAt := startAt.Add(15 * time.Minute)

			wattsProduced, err := svr.enphaseClient.FetchProduction(ctx, system.SystemID, session.AccessToken, startAt)
			if err != nil {
				log.Printf("failed to query production for system %d of user %s: %s", system.SystemID, session.UserID, err)
				continue
			}
			log.Printf("%d watts produced from %s to %s", wattsProduced, startAt, endAt)

			wattsConsumed, err := svr.enphaseClient.FetchConsumption(ctx, system.SystemID, session.AccessToken, startAt)
			if err != nil {
				log.Printf("failed to query consumption for system %d of user %s: %s", system.SystemID, session.UserID, err)
				continue
			}
			log.Printf("%d watts consumed from %s to %s", wattsConsumed, startAt, endAt)

			telemParams := storage.InsertEnphaseTelemetryParams{
				UserID:        session.UserID,
				SystemID:      system.SystemID,
				StartAt:       startAt,
				EndAt:         endAt,
				InsertedAt:    time.Now(),
				ProducedWatts: wattsProduced,
				ConsumedWatts: wattsConsumed,
			}
			err = storage.New(svr.db).InsertEnphaseTelemetry(ctx, telemParams)
			if err != nil {
				log.Printf("failed to save telemetry for system %d of user %s: %s", system.SystemID, session.UserID, err)
				continue
			}

			phase, err := powertrend.CheckForStateTransitions()
			if err != nil {
				log.Printf("failed to check for state transitions for system %d of user %s: %s", system.SystemID, session.UserID, err)
				continue
			}

			if phase == powertrend.NoChange {
				log.Printf("no state transition for system %d of user %s: %s", system.SystemID, session.UserID, err)
				continue
			}

			for _, row := range sinks {
				sendErr := svr.notifier.Send(ctx, powersinks.Channel(row.Channel), row.Recipient.String, phase)

				// always record the notification attempt, regardless of success/failure
				//if err := storage.InsertMessageAttempt(ctx, svr.db, powersink.NotifierID, phase, sendErr); err != nil {
				//	log.Printf("failed to write notification attempt to database: %s", err)
				//}

				if sendErr != nil {
					log.Printf("failed to notify powersink for system %d of user %s: %s", system.SystemID, session.UserID, sendErr)
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

func (svr *server) Host(r *http.Request) string {
	if svr.host != "" {
		return svr.host
	}
	return r.Host
}

func getFunctionName(i interface{}) string {
	return runtime.FuncForPC(reflect.ValueOf(i).Pointer()).Name()
}
