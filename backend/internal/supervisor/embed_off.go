//go:build !embed_timeplusd

package supervisor

// embeddedTimeplusd reports no embedded binary in the default build.
func embeddedTimeplusd() ([]byte, bool) { return nil, false }
