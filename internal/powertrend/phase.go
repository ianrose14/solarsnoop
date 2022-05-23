package powertrend

type Phase string

const (
	NoChange          Phase = "no-change"
	SurplusPowerStart Phase = "surplus-start"
	SurplusPowerStop  Phase = "surplus-stop"
)

func CheckForStateTransitions() (Phase, error) {
	// NOSUBMIT
	return SurplusPowerStart, nil
}
