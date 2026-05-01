package mirror

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Options struct {
	RepoPath string
	Remote   string
	Branch   string
	Git      string
}

func EnsureRepo(ctx context.Context, opts Options) error {
	opts = normalize(opts)
	if opts.RepoPath == "" {
		return errors.New("repo path is required")
	}
	if _, err := os.Stat(filepath.Join(opts.RepoPath, ".git")); err == nil {
		return nil
	}
	if opts.Remote != "" {
		if err := os.MkdirAll(filepath.Dir(opts.RepoPath), 0o755); err != nil {
			return fmt.Errorf("create repo parent: %w", err)
		}
		if err := run(ctx, "", opts.Git, "clone", opts.Remote, opts.RepoPath); err != nil {
			return err
		}
		if opts.Branch != "" {
			return run(ctx, opts.RepoPath, opts.Git, "checkout", "-B", opts.Branch)
		}
		return nil
	}
	if err := os.MkdirAll(opts.RepoPath, 0o755); err != nil {
		return fmt.Errorf("create repo path: %w", err)
	}
	if err := run(ctx, opts.RepoPath, opts.Git, "init"); err != nil {
		return err
	}
	if opts.Branch != "" {
		return run(ctx, opts.RepoPath, opts.Git, "checkout", "-B", opts.Branch)
	}
	return nil
}

func Pull(ctx context.Context, opts Options) error {
	opts = normalize(opts)
	if opts.Remote == "" {
		return EnsureRepo(ctx, opts)
	}
	if err := EnsureRepo(ctx, opts); err != nil {
		return err
	}
	if err := run(ctx, opts.RepoPath, opts.Git, "fetch", "--prune", "origin"); err != nil {
		return err
	}
	remoteRef := "refs/remotes/origin/" + opts.Branch
	if _, err := output(ctx, opts.RepoPath, opts.Git, "rev-parse", "--verify", remoteRef); err != nil {
		return run(ctx, opts.RepoPath, opts.Git, "checkout", "-B", opts.Branch)
	}
	return run(ctx, opts.RepoPath, opts.Git, "checkout", "-B", opts.Branch, "origin/"+opts.Branch)
}

func Commit(ctx context.Context, opts Options, message string) (bool, error) {
	opts = normalize(opts)
	if message == "" {
		message = "archive: update snapshot"
	}
	if err := run(ctx, opts.RepoPath, opts.Git, "add", "."); err != nil {
		return false, err
	}
	dirty, err := Dirty(ctx, opts)
	if err != nil {
		return false, err
	}
	if !dirty {
		return false, nil
	}
	if err := run(ctx, opts.RepoPath, opts.Git,
		"-c", "commit.gpgsign=false",
		"-c", "user.name=crawlkit",
		"-c", "user.email=crawlkit@example.invalid",
		"commit", "-m", message,
	); err != nil {
		return false, err
	}
	return true, nil
}

func Push(ctx context.Context, opts Options) error {
	opts = normalize(opts)
	out, err := output(ctx, opts.RepoPath, opts.Git, "push", "-u", "origin", opts.Branch)
	if err == nil {
		return nil
	}
	if !isNonFastForward(out) {
		return fmt.Errorf("git push: %w\n%s", err, strings.TrimSpace(out))
	}
	if pullErr := run(ctx, opts.RepoPath, opts.Git, "pull", "--rebase", "--autostash", "origin", opts.Branch); pullErr != nil {
		return fmt.Errorf("rebase before push retry: %w", pullErr)
	}
	return run(ctx, opts.RepoPath, opts.Git, "push", "-u", "origin", opts.Branch)
}

func Dirty(ctx context.Context, opts Options) (bool, error) {
	opts = normalize(opts)
	out, err := output(ctx, opts.RepoPath, opts.Git, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

func normalize(opts Options) Options {
	opts.RepoPath = strings.TrimSpace(opts.RepoPath)
	opts.Remote = strings.TrimSpace(opts.Remote)
	opts.Branch = strings.TrimSpace(opts.Branch)
	opts.Git = strings.TrimSpace(opts.Git)
	if opts.Branch == "" {
		opts.Branch = "main"
	}
	if opts.Git == "" {
		opts.Git = "git"
	}
	return opts
}

func run(ctx context.Context, dir, git string, args ...string) error {
	out, err := output(ctx, dir, git, args...)
	if err != nil {
		return fmt.Errorf("%s %s: %w\n%s", git, strings.Join(args, " "), err, strings.TrimSpace(out))
	}
	return nil
}

func output(ctx context.Context, dir, git string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, git, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func isNonFastForward(out string) bool {
	lower := strings.ToLower(out)
	return strings.Contains(lower, "non-fast-forward") ||
		strings.Contains(lower, "fetch first") ||
		strings.Contains(lower, "failed to push some refs")
}
