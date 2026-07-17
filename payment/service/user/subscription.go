package user

import (
	"context"
)

func (u *User) IsSubscriptionActive(ctx context.Context, params IsSubscriptionActiveParams) (bool, error) {
	if u == nil || u.subscription == nil {
		return false, ErrSubscriptionNotInitialized
	}
	return u.subscription.IsActive(ctx, params)
}
