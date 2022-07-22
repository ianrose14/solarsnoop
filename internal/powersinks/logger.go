package powersinks

import (
	"context"
	"fmt"
)

func processLoggerAction(ctx context.Context, fetcher Fetcher) Result {
	production, consumption, err := fetcher.fetchRecentMetering(ctx)
	if err != nil {
		return Result{
			Desired:        INFO,
			DesiredReason:  fmt.Sprintf("failed to fetch meter data: %+v", err),
			Executed:       INFO,
			ExecutedReason: "",
			Success:        true,
			SuccessReason:  "",
		}
	}

	if IsExcessProduction(production, consumption) {
		return Result{
			Desired:        CONSUME,
			DesiredReason:  fmt.Sprintf("%d production >> %d consumption", production, consumption),
			Executed:       CONSUME,
			ExecutedReason: "",
			Success:        true,
			SuccessReason:  "",
		}
	}

	if IsExcessConsumption(production, consumption) {
		return Result{
			Desired:        PRODUCE,
			DesiredReason:  fmt.Sprintf("%d consumption >> %d production", consumption, production),
			Executed:       PRODUCE,
			ExecutedReason: "",
			Success:        true,
			SuccessReason:  "",
		}
	}

	// else, production ~= consumption
	return Result{
		Desired:        NONE,
		DesiredReason:  fmt.Sprintf("%d production ~= %d consumption", production, consumption),
		Executed:       NONE,
		ExecutedReason: "",
		Success:        true,
		SuccessReason:  "",
	}
}
