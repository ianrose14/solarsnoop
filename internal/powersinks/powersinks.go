package powersinks

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/ianrose14/solarsnoop/internal/storage"
	"github.com/ianrose14/solarsnoop/internal/util"
	"github.com/ianrose14/solarsnoop/pkg/enphase"
)

var (
	// Obviously super rough guesses
	// TODO - make these shift with the seasons?  e.g. shorter days in winter?
	startOfSun = 9*time.Hour + 30*time.Minute // offset from start of day (midnight)
	endOfSun   = 5*time.Hour + 30*time.Minute // offset from start of day (midnight)
)

var (
	ErrMeterReadSkipped = errors.New("meter query skipped")
)

type Action string

const (
	CONSUME Action = "consume" // send a message to consume more energy
	PRODUCE Action = "produce" // send a message to produce more energy
	NONE    Action = "none"    // maintain the status quo; don't send any message
	INFO    Action = "info"    // send an informational message (e.g. to report an error)
)

func (a Action) IsMutative() bool {
	switch a {
	case CONSUME, PRODUCE:
		return true
	default:
		return false
	}
}

type Channel string

const (
	SMS    Channel = "sms"
	Email  Channel = "email"
	Ecobee Channel = "ecobee"
	Logger Channel = "logger"
)

func (c Channel) RequiresRecipient() bool {
	switch c {
	case SMS, Email:
		return true
	default:
		return false
	}
}

func (c Channel) IsValid() bool {
	switch c {
	case SMS, Email, Ecobee, Logger:
		return true
	default:
		return false
	}
}

type ExtApi struct {
	hostname       string
	sendgridApiKey string
}

func NewExtApi(hostname, sendgridApiKey string) *ExtApi {
	if hostname == "" {
		hostname = "www.solarsnoop.com"
	}
	return &ExtApi{
		hostname:       hostname,
		sendgridApiKey: sendgridApiKey,
	}
}

// Returns the current hostname, e.g. for constructing URLs.
func (e *ExtApi) Hostname() string {
	return e.hostname
}

func (e *ExtApi) sendEcobeeCommand(ctx context.Context, recipient string, action Action) error {
	return errors.New("unimplemented")
}

func (e *ExtApi) sendEmail(ctx context.Context, recipient, subject, body string) error {
	/*
		from := mail.NewEmail("Example User", "test@example.com")
			subject := "Sending with Twilio SendGrid is Fun"
			to := mail.NewEmail("Example User", "test@example.com")
			plainTextContent := "and easy to do anywhere, even with Go"
			htmlContent := "<strong>and easy to do anywhere, even with Go</strong>"
			message := mail.NewSingleEmail(from, subject, to, plainTextContent, htmlContent)
			client := sendgrid.NewSendClient(os.Getenv("SENDGRID_API_KEY"))
			response, err := client.Send(message)
			if err != nil {
				log.Println(err)
			} else {
				fmt.Println(response.StatusCode)
				fmt.Println(response.Body)
				fmt.Println(response.Headers)
			}
	*/
	return errors.New("unimplemented")
}

func (e *ExtApi) sendSMS(ctx context.Context, recipient string, action Action) error {
	return errors.New("unimplemented")
}

type Factory struct {
	fetchData                             func(context.Context) (int64, int64, error)
	fetchOnce                             sync.Once
	fetchedProduction, fetchedConsumption int64
	fetchError                            error

	sender *ExtApi
}

type Fetcher interface {
	fetchRecentMetering(ctx context.Context) (int64, int64, error)
}

func (f *Factory) Execute(ctx context.Context, row storage.QueryPowersinksForSystemRow, actions []storage.FetchRecentActionsRow) (*Result, error) {
	var result Result
	switch Channel(row.Channel) {
	case SMS:
		result = processSMSAction(ctx, row.Recipient.String)
	case Email:
		result = processEmailAction(ctx, row.Recipient.String, Fetcher(f), actions, f.sender)
	case Ecobee:
		result = processEcobeeAction(ctx)
	case Logger:
		processLoggerAction(ctx, Fetcher(f))
	default:
		return nil, fmt.Errorf("unsupported Channel: %q", row.Channel)
	}

	return &result, nil
}

func (f *Factory) fetchRecentMetering(ctx context.Context) (int64, int64, error) {
	// memoize results of calling fetchData so we only every do it once (limits enphase API usage)
	f.fetchOnce.Do(func() {
		f.fetchedProduction, f.fetchedConsumption, f.fetchError = f.fetchData(ctx)
	})
	return f.fetchedProduction, f.fetchedConsumption, f.fetchError
}

// TODO - should we make this past 1 hour?
// fetchData returns watts produced and watts consumed over the past (roughly) 15 minutes.
func fetchRecentMetering(ctx context.Context, enphaseClient *enphase.Client, system *storage.QueryEnphaseSystemsRow, session *storage.AuthSession) (int64, int64, error) {
	loc, err := time.LoadLocation(system.Timezone)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to load timezone %q: %w", system.Timezone, err)
	}

	now := time.Now().In(loc)
	dayOffset := util.OffsetIntoDay(now)
	if dayOffset < startOfSun || dayOffset > endOfSun {
		return 0, 0, fmt.Errorf("time at system (%s) is outside of primary solar hours: %w", now, ErrMeterReadSkipped)
	}

	// We want to start at the beginning of the last FULL 15 minute interval,
	// but also it can (anecdotally) take up to 5 minutes for data to appear.
	// So our start should be so somewhere between 20 and 35 minutes ago.
	startAt := time.Now().Add(-5 * time.Minute).Truncate(15 * time.Minute).Add(-15 * time.Minute)
	endAt := startAt.Add(15 * time.Minute)

	wattsProduced, err := enphaseClient.FetchProduction(ctx, system.SystemID, session.AccessToken, startAt)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to query production for system %d of user %s: %w", system.SystemID, session.UserID, err)
	}
	log.Printf("%d watts produced from %s to %s", wattsProduced, startAt, endAt)

	wattsConsumed, err := enphaseClient.FetchConsumption(ctx, system.SystemID, session.AccessToken, startAt)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to query consumption for system %d of user %s: %w", system.SystemID, session.UserID, err)
	}
	log.Printf("system %d: %d watts consumed from %s to %s", system.SystemID, wattsConsumed, startAt, endAt)
	return wattsProduced, wattsConsumed, nil
}

func NewFactory(enphaseClient *enphase.Client, system *storage.QueryEnphaseSystemsRow, session *storage.AuthSession, sender *ExtApi) *Factory {
	return &Factory{
		fetchData: func(ctx context.Context) (int64, int64, error) {
			return fetchRecentMetering(ctx, enphaseClient, system, session)
		},
		sender: sender,
	}
}

type Powersink interface {
	CalculateDesiredAction(getNetProduction func() (int64, int64, error)) (Action, string)
	Execute(getDesiredAction func() Action) Result
}

type Result struct {
	Desired        Action // What do we want to do, based on production+consumption
	DesiredReason  string
	Executed       Action // What did we actually try to do (e.g. based on recent actions)
	ExecutedReason string
	Success        bool // Did it work?
	SuccessReason  string
}

func IsExcessConsumption(production, consumption int64) bool {
	// TODO - also look at system capacity, via /api/v4/systems/{id}
	return consumption-production > 1000 // greater than 1KW net consumption
}

func IsExcessProduction(production, consumption int64) bool {
	// TODO - also look at system capacity, via /api/v4/systems/{id}
	return production-consumption > 1000 // greater than 1KW net production
}

func okToSendConsume(minTimeFromLastConsume time.Duration) bool {
	// TODO - implement
	return true
}

func getLastExecutedAction(actions []storage.FetchRecentActionsRow) (Action, time.Time, bool) {
	for _, action := range actions {
		if action.Success && Action(action.ExecutedAction).IsMutative() {
			return Action(action.ExecutedAction), action.Timestamp, true
		}
	}
	return "", time.Time{}, false
}
