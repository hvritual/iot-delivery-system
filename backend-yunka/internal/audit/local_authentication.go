package audit

import "context"

// RecordLocalAccessAuthenticationAccepted records acceptance of a YU-22 local
// access credential after signature/session verification. It is deliberately
// separate from RecordAuthenticationAccepted, whose reason code describes the
// older BFF assertion boundary.
func (recorder *SecurityRecorder) RecordLocalAccessAuthenticationAccepted(ctx context.Context, operation, transport string) error {
	actorType, actorID, organizationID, err := trustedSecurityActor(ctx)
	if err != nil {
		return err
	}
	return recorder.record(ctx, securityRecord{
		eventCategory:  EventCategoryAuthentication,
		actorType:      actorType,
		actorID:        actorID,
		organizationID: organizationID,
		operation:      operation,
		decision:       DecisionNotEvaluated,
		result:         ResultSuccess,
		reasonCode:     "authentication.local_access_accepted",
		transport:      transport,
		phase:          "authentication",
		failureClass:   "accepted",
		change:         "accepted",
	})
}
