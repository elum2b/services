package refund

import utils "github.com/elum2b/services/internal/utils"

func refIfNotEmpty(value string) *string {
	if value == "" {
		return nil
	}
	return utils.Ref(value)
}
