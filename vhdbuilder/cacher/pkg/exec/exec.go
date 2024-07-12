package exec

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/sethvargo/go-retry"
)

const (
	commandSeparator = " "

	defaultCommandTimeout = 10 * time.Second
	defaultCommandWait    = 3 * time.Second
)

func toPtr(d time.Duration) *time.Duration {
	return &d
}

type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

func (r *Result) Failed() bool {
	return r.ExitCode != 0
}

func (r *Result) AsError() error {
	if r.Failed() {
		return fmt.Errorf("code: %d, stderr: %s", r.ExitCode, r.Stderr)
	}
	return nil
}

func (r *Result) String() string {
	str := fmt.Sprintf("exit code: %d", r.ExitCode)
	if r.Stdout != "" {
		str = str + fmt.Sprintf("\n--------------stdout--------------%s", r.Stdout)
	}
	if r.Stderr != "" {
		str = str + fmt.Sprintf("\n--------------stderr--------------%s", r.Stderr)
	}
	if r.Stdout != "" || r.Stderr != "" {
		str = str + "----------------------------------"
	}
	return str
}

func fromExitError(err *exec.ExitError) *Result {
	return &Result{
		Stderr:   string(err.Stderr),
		ExitCode: err.ExitCode(),
	}
}

type CommandConfig struct {
	Timeout    *time.Duration
	Wait       *time.Duration
	MaxRetries int
}

func (cc *CommandConfig) validate() {
	if cc == nil {
		return
	}
	if cc.Timeout != nil {
		cc.Timeout = toPtr(defaultCommandTimeout)
	}
	if cc.Wait == nil {
		cc.Wait = toPtr(defaultCommandWait)
	}
	if cc.MaxRetries < 0 {
		cc.MaxRetries = 0
	}
}

type Command struct {
	raw  string
	app  string
	args []string
	cfg  *CommandConfig
}

func NewCommand(commandString string, cfg *CommandConfig) (*Command, error) {
	cfg.validate()
	if commandString == "" {
		return nil, fmt.Errorf("cannot execute empty command")
	}

	parts := strings.Split(commandString, commandSeparator)
	if len(parts) < 2 {
		return nil, fmt.Errorf("specified command %q is malformed, expected to be in format \"app args...\"", commandString)
	}

	return &Command{
		raw:  commandString,
		app:  parts[0],
		args: parts[1:],
		cfg:  cfg,
	}, nil
}

func (c *Command) Execute() (*Result, error) {
	if c.cfg == nil {
		return execute(c)
	}
	if c.cfg.MaxRetries > 0 {
		return executeWithRetries(c)
	}
	return executeWithTimeout(c)
}

func execute(c *Command) (*Result, error) {
	cmd := exec.Command(c.app, c.args...)

	stdout, err := cmd.Output()
	if err != nil {
		var exitError *exec.ExitError
		if !errors.As(err, &exitError) {
			return nil, fmt.Errorf("executing command %q: %w", c.raw, err)
		}
		return fromExitError(exitError), nil
	}

	return &Result{
		Stdout: string(stdout),
	}, nil
}

func executeWithTimeout(c *Command) (*Result, error) {
	ch := make(chan struct {
		err error
		res *Result
	})

	ctx, cancel := context.WithTimeout(context.Background(), *c.cfg.Timeout)
	defer cancel()

	// TODO(cameissner): are these potentially leaky?
	go func() {
		res, err := execute(c)
		ch <- struct {
			err error
			res *Result
		}{err: err, res: res}
	}()

	select {
	case r := <-ch:
		return r.res, r.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// executeWithRetries attempts to emulate: https://github.com/Azure/AgentBaker/blob/master/parts/linux/cloud-init/artifacts/cse_helpers.sh#L133-L145
func executeWithRetries(c *Command) (*Result, error) {
	backoff := retry.WithMaxRetries(uint64(c.cfg.MaxRetries), retry.NewConstant(*c.cfg.Wait))
	var res *Result
	err := retry.Do(context.Background(), backoff, func(ctx context.Context) error {
		var err error
		res, err = executeWithTimeout(c)
		if err != nil {
			// retry if the command itself timed out
			if errors.Is(err, context.DeadlineExceeded) {
				return retry.RetryableError(err)
			}
			// don't retry if we weren't able to execute the command at all
			return err
		}
		if err = res.AsError(); err != nil {
			// blindly retry in the case where the command executed
			// but ended up failing
			return retry.RetryableError(err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return res, nil
}
