package supervisor

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

// DefaultDevCredentials are the credentials a directly-launched `timeplusd server`
// accepts out of the box: the built-in `default` user with an empty password.
// AgentGuard launches the engine itself, bypassing the timeplus-enterprise image
// entrypoint that would otherwise provision the `proton` user — so `proton` does
// not exist in all-in-one mode. This is local child ownership for dev / gated
// preview only and is explicitly NOT production-safe: a no-password default user
// behind exposed engine ports is wide open. GA must use generated credentials
// and/or bind the engine to loopback — see spec §5.5 / §10.
func DefaultDevCredentials() StaticCredentials {
	return NewStaticCredentials("default", "")
}
