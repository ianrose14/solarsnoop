package powersinks

type ecobeePowersink struct {
}

func (s *ecobeePowersink) CalculateDesiredAction(getNetProduction func() (int64, int64, error)) (Action, string) {
	return NONE, ""
}
