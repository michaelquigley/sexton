package agent

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"

	"github.com/michaelquigley/df/dl"
	"github.com/michaelquigley/sexton/internal/config"
)

func (a *Agent) runHooks(ctx context.Context, phase string, hooks []*config.ResolvedHook) error {
	if len(hooks) == 0 {
		return nil
	}

	for i, hook := range hooks {
		dl.Infof("[%s] running hook %d/%d: %s", phase, i+1, len(hooks), hook.Command)

		hookCtx, cancel := context.WithTimeout(ctx, hook.Timeout)

		cmd := exec.CommandContext(hookCtx, "sh", "-c", hook.Command)
		if hook.Dir != "" {
			cmd.Dir = hook.Dir
		} else {
			cmd.Dir = a.cfg.Path
		}
		cmd.Env = append(cmd.Environ(),
			"SEXTON_REPO_PATH="+a.cfg.Path,
			"SEXTON_REPO_NAME="+a.cfg.Name,
			"SEXTON_HOOK="+phase,
		)
		for k, v := range hook.Env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}

		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		start := time.Now()

		err := cmd.Run()
		hookErr := hookCtx.Err()
		cancel()

		if out := stdout.String(); out != "" {
			dl.Debugf("[%s] stdout: %s", phase, out)
		}
		if errOut := stderr.String(); errOut != "" {
			dl.Debugf("[%s] stderr: %s", phase, errOut)
		}

		if err != nil {
			if hookErr != nil {
				return fmt.Errorf("%s hook failed: command=%q: %w", phase, hook.Command, hookErr)
			}
			exitCode := -1
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			}
			if errors.Is(err, context.Canceled) {
				return fmt.Errorf("%s hook failed: command=%q: %w", phase, hook.Command, err)
			}
			return fmt.Errorf("%s hook failed: command=%q exit_code=%d stderr=%q: %w",
				phase, hook.Command, exitCode, stderr.String(), err)
		}

		dl.Infof("[%s] hook %d/%d completed successfully (in %v)", phase, i+1, len(hooks), time.Since(start))
	}

	return nil
}
