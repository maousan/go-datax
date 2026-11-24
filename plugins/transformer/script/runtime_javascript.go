package script

import (
	"context"
	"fmt"
	"time"

	scriptEngine "github.com/tx7do/go-scripts"
	_ "github.com/tx7do/go-scripts/javascript"
)

func init() {
	registerLanguage("javascript", languageSpec{
		factory:         newJavaScriptRuntime,
		supportsTimeout: true,
	})
}

// javaScriptRuntime executes a script that defines function transform().
type javaScriptRuntime struct {
	code    string
	api     *recordAPI
	timeout time.Duration
	engine  scriptEngine.Engine
}

func newJavaScriptRuntime(code string, api *recordAPI, timeout time.Duration) (scriptRuntime, error) {
	return &javaScriptRuntime{code: code, api: api, timeout: timeout}, nil
}

func (r *javaScriptRuntime) Prepare() error {
	ctx := context.Background()
	if r.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.timeout)
		defer cancel()
	}

	engine, err := scriptEngine.NewScriptEngine(scriptEngine.JavaScriptType)
	if err != nil {
		return fmt.Errorf("create javascript engine: %v", err)
	}
	r.engine = engine

	if err := engine.Init(ctx); err != nil {
		return fmt.Errorf("init javascript engine: %v", err)
	}

	for name, function := range r.api.functions() {
		if err := engine.RegisterFunction(name, function); err != nil {
			return fmt.Errorf("register function %s: %v", name, err)
		}
	}

	// LoadString only compiles; Execute runs top-level code once, which is
	// where the script defines transform().
	if err := engine.LoadString(ctx, "dx_script.js", r.code); err != nil {
		return fmt.Errorf("compile script: %v", err)
	}
	if _, err := engine.Execute(ctx); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("top-level script timed out after %s: %v", r.timeout, err)
		}
		return fmt.Errorf("run script: %v", err)
	}

	return nil
}

func (r *javaScriptRuntime) Transform() (bool, error) {
	ctx := context.Background()
	if r.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.timeout)
		defer cancel()
	}

	result, err := r.engine.CallFunction(ctx, "transform")
	if err != nil {
		if r.timeout > 0 && ctx.Err() != nil {
			return false, fmt.Errorf("script timed out after %s: %v", r.timeout, err)
		}
		return false, err
	}

	// Returning false filters the record; any other value (including null or
	// no return) keeps it.
	if filtered, ok := result.(bool); ok && !filtered {
		return false, nil
	}
	return true, nil
}

func (r *javaScriptRuntime) Close() error {
	if r.engine == nil {
		return nil
	}
	return r.engine.Close()
}
