package task

// How a task reports a decision Jev was required to make and did not.
//
// The six routing-capable judgment sites decide which way a task goes. When
// one of them is configured at TierRouting and cannot get an answer, the task
// stops rather than proceeding on an assumed one — see judgment.Required for
// why the configured tier, not the site alone, is what makes the difference.
//
// The landing state is StateBlocked, which is not terminal. That is the
// accurate description of every class here: an outage, a rate limit and a
// rejected credential all mean "this task cannot continue as configured right
// now", and all of them are resolved by an operator rather than by the task
// trying harder. A terminal StateFailed would claim the work itself failed,
// which is not what happened and would lose a resumable attempt.

import (
	"fmt"

	"github.com/akynte/boundedcode/internal/judgment"
)

// requiredDecisionReason is the sentence a task blocked on a required
// judgment carries.
//
// It names the site, the class, why this stopped the task rather than being
// logged, and what the operator does next — the last of which differs by
// class, because waiting helps with an outage and never helps with a bad
// credential.
func requiredDecisionReason(site string, class judgment.FailureClass, err error) string {
	next := "resolve the judgment configuration, then retry the task"
	if class.Retryable() {
		next = "retry the task once the service is reachable again"
	}
	return fmt.Sprintf("%v; %s is configured at routing, where its finding changes what the "+
		"task does, so the task stops rather than assume one — %s", err, site, next)
}
