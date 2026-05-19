package updates

// Helpers de testing — sólo compilados con `go test`. Exponen
// funciones unexported para que checker_test.go (que vive en
// package updates_test) pueda testearlas sin tener que exportarlas
// para uso real.

func IsNewerForTest(remote, local string) bool {
	return isNewer(remote, local)
}
