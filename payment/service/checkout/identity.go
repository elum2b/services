package checkout

import services "github.com/elum2b/services"

func actorPlatformID(actor *services.Actor) *int64 {
	if actor == nil {
		return nil
	}
	return &actor.PlatformID
}

func actorPlatformUserID(actor *services.Actor) *string {
	if actor == nil {
		return nil
	}
	return &actor.PlatformUserID
}

func actorInternalUserID(actor *services.Actor) *int64 {
	if actor == nil {
		return nil
	}
	return actor.InternalUserID
}
