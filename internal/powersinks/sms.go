package powersinks

type smsPowersink struct {
}

func (s *smsPowersink) CalculateDesiredAction(getNetProduction func() (int64, int64, error)) (Action, string) {
	return NONE, ""
}
