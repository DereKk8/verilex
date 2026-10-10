//go:build !darwin

package sandbox

// endSandbox has nothing to do here: on Linux every process of the sandbox, whatever its session,
// dies with bwrap's pid namespace when the launcher ends bwrap.
func endSandbox() {}

func contained() string { return "" }
