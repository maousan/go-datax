package script

import (
	"context"
	"fmt"
	"time"

	scriptEngine "github.com/tx7do/go-scripts"
	_ "github.com/tx7do/go-scripts/expr"
)

func init() {
	registerLanguage("expr", languageSpec{
		factory:         newExprRuntime,
		supportsTimeout: false,
	})
}

// exprRuntime evaluates a single Expr expression per record. Mutations are
// performed by calling the host functions; a false result filters the record.
type exprRuntime struct {
	code   string
	api    *recordAPI
	engine scriptEngine.Engine
}

func newExprRuntime(code string, api *recordAPI, _ time.Duration) (scriptRuntime, error) {
	return &exprRuntime{code: code, api: api}, nil
}

func (r *exprRuntime) Prepare() error {
	ctx := context.Background()

	engine, err := scriptEngine.NewScriptEngine(scriptEngine.ExprType)
	if err != nil {
		return fmt.Errorf("create expr engine: %v", err)
	}
	r.engine = engine

	if err := engine.Init(ctx); err != nil {
		return fmt.Errorf("init expr engine: %v", err)
	}

	// Functions must be registered before LoadString because Expr compiles
	// against the current environment.
	for name, function := range r.api.functions() {
		if err := engine.RegisterFunction(name, function); err != nil {
			return fmt.Errorf("register function %s: %v", name, err)
		}
	}

	if err := engine.LoadString(ctx, "dx_script.expr", r.code); err != nil {
		return fmt.Errorf("compile expression: %v", err)
	}

	return nil
}

func (r *exprRuntime) Transform() (bool, error) {
	result, err := r.engine.Execute(context.Background())
	if err != nil {
		return false, err
	}

	// A false expression result filters the record; other values keep it.
	if filtered, ok := result.(bool); ok && !filtered {
		return false, nil
	}
	return true, nil
}

func (r *exprRuntime) Close() error {
	if r.engine == nil {
		return nil
	}
	return r.engine.Close()
}
