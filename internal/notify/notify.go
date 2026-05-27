// Package notify sends fire-and-forget notifications when a booking is
// confirmed. Decoupled from the calendar provider so it works regardless
// of which backend the user runs.
package notify

import "context"

// Notifier abstracts the transport. SMTP is the only implementation today.
type Notifier interface {
	Notify(ctx context.Context, subject, body string) error
}

// Disabled is a no-op notifier returned when notifications.smtp.enabled=false.
type Disabled struct{}

func (Disabled) Notify(_ context.Context, _, _ string) error { return nil }
