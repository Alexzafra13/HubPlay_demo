package upload

import "context"

// RunSweepForTests es un thin wrapper sobre el método privado sweep
// para que los tests del package upload_test (black box) puedan
// invocarlo sin esperar al ticker. Vive en un archivo _test.go con
// build tag implícito, así no se exporta al binario de producción.
func RunSweepForTests(gc *GC, ctx context.Context) {
	gc.sweep(ctx)
}
