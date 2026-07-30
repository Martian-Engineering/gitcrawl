package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/openclaw/gitcrawl/internal/capture"
)

// runCapture exports one deterministic code-free repository snapshot.
func (a *App) runCapture(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("capture", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	schema := fs.String("schema", capture.SchemaV1, "capture schema")
	output := fs.String("output", "", "write capture atomically to this path")
	since := fs.String("since", "", "include threads updated at or after this RFC3339 instant")
	jsonOut := fs.Bool("json", false, "write JSON output")
	if err := fs.Parse(normalizeCommandArgs(args, map[string]bool{
		"schema": true,
		"output": true,
		"since":  true,
	})); err != nil {
		return usageErr(err)
	}
	a.applyCommandJSON(*jsonOut)
	if fs.NArg() != 1 {
		return usageErr(fmt.Errorf("capture requires owner/repo"))
	}
	if strings.TrimSpace(*schema) != capture.SchemaV1 {
		return usageErr(fmt.Errorf(
			"unsupported capture schema %q",
			strings.TrimSpace(*schema),
		))
	}
	owner, repoName, err := parseOwnerRepo(fs.Arg(0))
	if err != nil {
		return usageErr(err)
	}
	rt, err := a.openLocalRuntimeReadOnly(ctx)
	if err != nil {
		return err
	}
	defer rt.Store.Close()
	result, err := capture.Build(
		ctx,
		rt.Store,
		owner+"/"+repoName,
		version,
		*since,
	)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("encode capture: %w", err)
	}
	data = append(data, '\n')
	if strings.TrimSpace(*output) == "" {
		_, err = a.Stdout.Write(data)
		return err
	}
	return writeCaptureAtomically(*output, data)
}

// writeCaptureAtomically publishes a private file without a partial target.
func writeCaptureAtomically(output string, data []byte) error {
	output = filepath.Clean(strings.TrimSpace(output))
	if output == "." || output == "" {
		return fmt.Errorf("capture output path is empty")
	}
	parent := filepath.Dir(output)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return fmt.Errorf("create capture output directory: %w", err)
	}
	temp, err := os.CreateTemp(parent, "."+filepath.Base(output)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create capture output: %w", err)
	}
	tempPath := temp.Name()
	published := false
	defer func() {
		_ = temp.Close()
		if !published {
			_ = os.Remove(tempPath)
		}
	}()
	if err := temp.Chmod(0o600); err != nil {
		return fmt.Errorf("protect capture output: %w", err)
	}
	if _, err := temp.Write(data); err != nil {
		return fmt.Errorf("write capture output: %w", err)
	}
	if err := temp.Sync(); err != nil {
		return fmt.Errorf("sync capture output: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close capture output: %w", err)
	}
	if err := os.Rename(tempPath, output); err != nil {
		return fmt.Errorf("publish capture output: %w", err)
	}
	published = true
	return nil
}
