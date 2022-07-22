package powersinks

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ianrose14/solarsnoop/internal/storage"
	"github.com/ianrose14/solarsnoop/internal/util"
)

const (
	// How much time must pass after a CONSUME before we'll send another CONSUME.
	emailMinConsumeToConsume = 4*time.Hour - 15*time.Minute // subtract some slack to account for clock tick sync

	// How much time must pass after a PRODUCE before we'll send a CONSUME.
	emailMinProduceToConsume = 2*time.Hour - 15*time.Minute // subtract some slack to account for clock tick sync

	// How much time must pass after a PRODUCE before we'll send another PRODUCE.
	emailMinProduceToProduce = 4*time.Hour - 15*time.Minute // subtract some slack to account for clock tick sync
)

func processEmailAction(ctx context.Context, recipient string, fetcher Fetcher, actions []storage.FetchRecentActionsRow, sender *ExtApi) Result {
	// early out if we know there's nothing we will want to do - important to do this before calling fetcher since
	// fetcher uses up enlighten API quota
	lastAction, lastTime, hasLast := getLastExecutedAction(actions)
	sinceLast := time.Since(lastTime)
	if hasLast {
		thresh := util.Min(emailMinProduceToConsume, emailMinConsumeToConsume)
		if sinceLast < thresh {
			return Result{
				Desired:       NONE,
				DesiredReason: fmt.Sprintf("Time since last action (%s) is %s which is too recent.", lastAction, sinceLast),
				Executed:      NONE,
				Success:       true,
			}
		}
	}

	production, consumption, err := fetcher.fetchRecentMetering(ctx)
	if errors.Is(err, ErrMeterReadSkipped) {
		return Result{
			Desired:       NONE,
			DesiredReason: err.Error(),
			Executed:      NONE,
			Success:       true,
		}
	}
	if err != nil {
		result := Result{
			Desired:       INFO,
			DesiredReason: fmt.Sprintf("failed to fetch meter data: %+v", err),
			Executed:      INFO,
		}
		sendErr := sender.sendEmail(ctx, recipient, "Error communicating with Enlighten",
			"Attention: We are currently unable to communicate with Enphase's \"Enlighten\""+
				" API for production & consumption data from your system.")
		if sendErr != nil {
			result.Success = false
			result.SuccessReason = fmt.Sprintf("failed to send email to %q: %+v", recipient, sendErr)
		} else {
			result.Success = true
		}

		return result
	}

	desired, desiredReason := func() (Action, string) {
		if IsExcessProduction(production, consumption) {
			return CONSUME, fmt.Sprintf("%d production >> %d consumption", production, consumption)
		} else if IsExcessConsumption(production, consumption) {
			return PRODUCE, fmt.Sprintf("%d consumption >> %d production", consumption, production)
		} else {
			return NONE, fmt.Sprintf("%d production ~= %d consumption", production, consumption)
		}
	}()

	executed, executedReason := func() (Action, string) {
		if !hasLast {
			return desired, "no prior actions"
		}

		switch desired {
		case CONSUME:
			if lastAction == CONSUME {
				if sinceLast > emailMinConsumeToConsume {
					return CONSUME, fmt.Sprintf("%s since last action (CONSUME)", sinceLast)
				}
				return NONE, fmt.Sprintf("%s since last action (CONSUME), too recent", sinceLast)
			}
			if lastAction == PRODUCE {
				if sinceLast > emailMinProduceToConsume {
					return CONSUME, fmt.Sprintf("%s since last action (PRODUCE)", sinceLast)
				}
				return NONE, fmt.Sprintf("%s since last action (PRODUCE), too recent", sinceLast)
			}
		case PRODUCE:
			if lastAction == CONSUME {
				return PRODUCE, fmt.Sprintf("last action was PRODUCE")
			}
			if lastAction == PRODUCE {
				if sinceLast > emailMinProduceToProduce {
					return PRODUCE, fmt.Sprintf("%s since last action (PRODUCE)", sinceLast)
				}
				return NONE, fmt.Sprintf("%s since last action (PRODUCE), too recent", sinceLast)
			}
		}
		return desired, ""
	}()

	result := Result{
		Desired:        desired,
		DesiredReason:  desiredReason,
		Executed:       executed,
		ExecutedReason: executedReason,
	}

	if executed != NONE {
		var subj, body string
		if executed == CONSUME {
			// TODO - timeframe wording
			subj = "Your solar panels are underproducing - time to reduce usage."
			body = fmt.Sprintf("Over the past hour your solar panels produced %d Watts of electricity, but your home consumed %d Watts.  Consider reducing usage!  Visit https://%s/tips/reduce for tips.",
				production, consumption, sender.Hostname())
		} else if executed == PRODUCE {
			subj = "Your solar panels are overproducing - time to increase usage."
			body = fmt.Sprintf("Over the past hour your solar panels produced %d Watts of electricity, but your home only consumed %d Watts.  Consider increasing usage!  Visit https://%s/tips/produce for tips.",
				production, consumption, sender.Hostname())
		}

		if err := sender.sendEmail(ctx, recipient, subj, body); err != nil {
			result.Success = false
			result.SuccessReason = fmt.Sprintf("failed to send email to %q: %+v", recipient, err)
		} else {
			result.Success = true
			result.SuccessReason = fmt.Sprintf("sent email to %q", recipient)
		}
	}

	return result
}
