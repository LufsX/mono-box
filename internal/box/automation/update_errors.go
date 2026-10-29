package automation

import (
	"context"
	"errors"
	"strings"

	"github.com/LufsX/mono-box/internal/box/storage"
)

func safeUpdateError(err error) string {
	if errors.Is(err, context.Canceled) {
		return "subscription update cancelled"
	}
	var reasons []string
	for _, reason := range []error{
		storage.ErrScheduledInventory, storage.ErrScheduledSource,
		storage.ErrScheduledDownload, storage.ErrScheduledConflict,
		storage.ErrScheduledCore, storage.ErrScheduledValidation,
	} {
		if errors.Is(err, reason) {
			reasons = append(reasons, reason.Error())
		}
	}
	if len(reasons) == 0 {
		return "subscription update failed"
	}
	return strings.Join(reasons, "; ")
}
