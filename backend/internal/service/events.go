package service

// EventSink lets the service layer emit live events without depending on
// how they're transported. internal/wshub's Hub implements this purely by
// matching method signatures (structural typing) — neither package imports
// the other, avoiding a service <-> api/wshub import cycle.
type EventSink interface {
	// LookupOccurred fires once per /api/lookup call (HTTP or WS). Called
	// from internal/api's performLookup, not from Lookup() itself — Lookup()
	// only knows the badge, not which named key called it (that's an
	// HTTP/WS-layer concern via auth.APIKeyFromContext).
	LookupOccurred(keyName, uidHex, login, coalitionName, coalitionColor string, found bool)

	// RefreshProgress fires repeatedly during a bulk CA/intra/coalitions
	// refresh. job is "ca" | "intra" | "coalitions". total may be -1 for
	// "ca" before the CA API's first page reports its count.
	RefreshProgress(job string, current, total int, currentItem string)
	RefreshComplete(job string, count int)
	RefreshError(job string, message string)
}
