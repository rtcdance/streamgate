package load

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// WaitForSamplingRelease lets the controller prepare faults after warmup and
// before the sampling clock starts. It is a test-harness file barrier only.
func WaitForSamplingRelease(ctx context.Context, dir string, timeout time.Duration) error {
	if dir == "" || timeout <= 0 {
		return fmt.Errorf("sampling barrier needs a directory and positive timeout")
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	for _, name := range []string{"ready.json", "release"} {
		if _, err := os.Lstat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			return fmt.Errorf("sampling barrier directory is not fresh")
		}
	}
	raw, err := json.Marshal(map[string]any{"event": "sampling.ready", "timestamp": time.Now().UTC()})
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(dir, "ready-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(temporary.Name())
	if _, err := temporary.Write(raw); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporary.Name(), filepath.Join(dir, "ready.json")); err != nil {
		return err
	}
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		info, err := os.Lstat(filepath.Join(dir, "release"))
		if err == nil {
			if !info.Mode().IsRegular() {
				return fmt.Errorf("sampling release is not a regular file")
			}
			return ctx.Err()
		}
		if !os.IsNotExist(err) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
