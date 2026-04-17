package runtime

import (
	"context"
	"fmt"

	"vibelang/internal/ast"
)

type withContext interface {
	Name() string
	Enter(context.Context, *Interpreter) (any, error)
	Exit(context.Context, *Interpreter, error) error
}

type managedContext struct {
	name  string
	enter func(context.Context, *Interpreter) (any, error)
	exit  func(context.Context, *Interpreter, error) error
}

func (m *managedContext) Name() string {
	return m.name
}

func (m *managedContext) Enter(ctx context.Context, interpreter *Interpreter) (any, error) {
	return m.enter(ctx, interpreter)
}

func (m *managedContext) Exit(ctx context.Context, interpreter *Interpreter, priorErr error) error {
	return m.exit(ctx, interpreter, priorErr)
}

func newManagedContext(name string, enter func(context.Context, *Interpreter) (any, error), exit func(context.Context, *Interpreter, error) error) *managedContext {
	return &managedContext{
		name:  name,
		enter: enter,
		exit:  exit,
	}
}

func (i *Interpreter) executeWithStatement(ctx context.Context, env *Environment, statement *ast.WithStmt, moduleDir string) (controlSignal, error) {
	rawContext, err := i.evaluateExpression(ctx, env, statement.Context)
	if err != nil {
		return signalNone, err
	}

	manager, ok := rawContext.(withContext)
	if !ok {
		return signalNone, fmt.Errorf("with expects a managed context, got %s", typeName(rawContext))
	}

	value, err := manager.Enter(ctx, i)
	if err != nil {
		return signalNone, fmt.Errorf("enter %s: %w", manager.Name(), err)
	}

	if statement.Target != nil {
		if err := i.assignValue(ctx, env, statement.Target, value); err != nil {
			exitErr := manager.Exit(ctx, i, err)
			return signalNone, combineWithExitError(err, manager.Name(), exitErr)
		}
	}

	signal, runErr := i.executeBlock(ctx, env, statement.Body, moduleDir)
	exitErr := manager.Exit(ctx, i, runErr)
	if exitErr != nil {
		return signalNone, combineWithExitError(runErr, manager.Name(), exitErr)
	}
	if runErr != nil {
		return signalNone, runErr
	}
	return signal, nil
}

func combineWithExitError(priorErr error, name string, exitErr error) error {
	if exitErr == nil {
		return priorErr
	}
	if priorErr == nil {
		return fmt.Errorf("exit %s: %w", name, exitErr)
	}
	return fmt.Errorf("%v; exit %s: %v", priorErr, name, exitErr)
}
