package mizudi

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	koanf "github.com/knadh/koanf/v2"
	do "github.com/samber/do/v2"
)

type attr string
type Injector = do.Injector

var injector = do.New()

const (
	SERVICE attr = "service"
)

func Register[T any](provider do.Provider[T]) func() T {
	do.Provide(injector, provider)
	return func() T { return do.MustInvoke[T](injector) }
}

func Retrieve[T any](attrs ...attr) (T, error) {
	if len(attrs) == 0 {
		return do.Invoke[T](injector)
	}
	var v T
	var servicePath = PackagePath()

	k, err := do.Invoke[*koanf.Koanf](injector)
	if err != nil {
		slog.Error("failed to di for koanf", "error", err)
		return v, err
	}

	kconf, err := do.Invoke[koanf.UnmarshalConf](injector)
	if err != nil {
		slog.Error("failed to di for koanf unmarshal conf", "error", err)
		return v, err
	}

	if err := k.UnmarshalWithConf(servicePath, &v, kconf); err != nil {
		slog.Error("failed to unmarshal config", "error", err)
		return v, err
	}

	return v, nil
}

func MustRetrieve[T any](attrs ...attr) T {
	if len(attrs) == 0 {
		return do.MustInvoke[T](injector)
	}
	if v, err := Retrieve[T](attrs...); err != nil {
		panic(err)
	} else {
		return v
	}
}

func PackagePath() string {
	wd, err := os.Getwd()
	if err != nil {
		panic(err)
	}

	pkgName := filepath.Base(wd)
	_, file, _, ok := runtime.Caller(1)
	if !ok {
		panic("failed to get runtime caller")
	}

	innerPaths := strings.SplitAfter(file, pkgName)
	fmt.Println("innerPaths", innerPaths, file, pkgName)
	if len(innerPaths) < 2 {
		panic("failed to get package path")
	}

	path := strings.Trim(filepath.Dir(innerPaths[len(innerPaths)-1]), "/")
	return strings.Replace(path, "svc", "service", 1)
}
