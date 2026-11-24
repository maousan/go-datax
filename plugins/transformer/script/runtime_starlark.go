package script

import (
	"context"
	"fmt"
	"time"

	scriptEngine "github.com/tx7do/go-scripts"
	_ "github.com/tx7do/go-scripts/starlark"
)

func init() {
	registerLanguage("starlark", languageSpec{
		factory:         newStarlarkRuntime,
		supportsTimeout: false,
	})
}

// starlarkRuntime executes a script that defines def transform(): ...
type starlarkRuntime struct {
	code   string
	api    *recordAPI
	engine scriptEngine.Engine
}

func newStarlarkRuntime(code string, api *recordAPI, _ time.Duration) (scriptRuntime, error) {
	return &starlarkRuntime{code: code, api: api}, nil
}

func (r *starlarkRuntime) Prepare() error {
	ctx := context.Background()

	engine, err := scriptEngine.NewScriptEngine(scriptEngine.StarlarkType)
	if err != nil {
		return fmt.Errorf("create starlark engine: %v", err)
	}
	r.engine = engine

	if err := engine.Init(ctx); err != nil {
		return fmt.Errorf("init starlark engine: %v", err)
	}

	for name, function := range r.api.functions() {
		if err := engine.RegisterFunction(name, function); err != nil {
			return fmt.Errorf("register function %s: %v", name, err)
		}
	}

	if err := engine.LoadString(ctx, "dx_script.star", r.code); err != nil {
		return fmt.Errorf("compile script: %v", err)
	}
	// Execute defines top-level functions such as transform().
	if _, err := engine.Execute(ctx); err != nil {
		return fmt.Errorf("run script: %v", err)
	}

	return nil
}

func (r *starlarkRuntime) Transform() (bool, error) {
	result, err := r.engine.CallFunction(context.Background(), "transform")
	if err != nil {
		return false, err
	}

	// Returning False filters the record; anything else (including None) keeps it.
	if filtered, ok := result.(bool); ok && !filtered {
		return false, nil
	}
	return true, nil
}

func (r *starlarkRuntime) Close() error {
	if r.engine == nil {
		return nil
	}
	return r.engine.Close()
}
