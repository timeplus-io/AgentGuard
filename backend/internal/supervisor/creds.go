package supervisor

import (
	"os"
	"path/filepath"
)

// TimeplusCredentials is the seam (spec §5.5, "SupervisorTimeplusCredentials")
// that yields the credentials the child timeplusd is launched with and that
// AgentGuard's own client uses. v1 ships a static provider; a generated/rotated
// provider can replace it later without changing callers.
type TimeplusCredentials interface {
	User() string
	Password() string
}

type StaticCredentials struct {
	user, password string
}

func NewStaticCredentials(user, password string) StaticCredentials {
	return StaticCredentials{user: user, password: password}
}

func (c StaticCredentials) User() string     { return c.user }
func (c StaticCredentials) Password() string { return c.password }

// DefaultDevCredentials are the credentials a directly-launched Timeplus server
// accepts out of the box: the built-in `default` user with an empty password.
// This is local child ownership for dev / gated preview only and is explicitly
// NOT production-safe when engine ports are exposed beyond loopback. GA must use
// generated credentials and/or keep the engine bound to loopback — see spec
// §5.5 / §10.
func DefaultDevCredentials() StaticCredentials {
	return NewStaticCredentials("default", "")
}

// DefaultCredentialsFromEnv selects credentials for the packaged Timeplus
// Enterprise image when TIMEPLUSD_CONFIG points at that image config; a runtime
// TIMEPLUSD_ADMIN_PASSWORD overrides the image's built-in fallback.
func DefaultCredentialsFromEnv() StaticCredentials {
	if password := os.Getenv("TIMEPLUSD_ADMIN_PASSWORD"); password != "" {
		return NewStaticCredentials("proton", password)
	}
	if filepath.Clean(os.Getenv("TIMEPLUSD_CONFIG")) == "/timeplus/conf/timeplusd.yaml" {
		return NewStaticCredentials("proton", "replaceme")
	}
	return DefaultDevCredentials()
}
