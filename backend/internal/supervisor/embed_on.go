//go:build embed_timeplusd

package supervisor

import _ "embed"

// timeplusdBinary is the embedded engine. Build with:
//
//	make backend-build-embedded-timeplusd
//
// which copies a linux/amd64 timeplusd to backend/internal/supervisor/timeplusd.bin
// before compiling with -tags embed_timeplusd.
//
//go:embed timeplusd.bin
var timeplusdBinary []byte

func embeddedTimeplusd() ([]byte, bool) { return timeplusdBinary, len(timeplusdBinary) > 0 }
