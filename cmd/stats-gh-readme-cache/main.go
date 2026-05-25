// Command stats-gh-readme-cache updates profile README image URLs with a
// cache key so GitHub refreshes generated SVG assets.
package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"

	internalreadmecache "github.com/agoodkind/stats/internal/readmecache"
)

const profileReadmePath = "profile/README.md"

func main() {
	slog.Info("stats-gh-readme-cache starting")
	if err := run(os.Args[1:]); err != nil {
		slog.Error("stats-gh-readme-cache failed", "err", err)
		_, _ = fmt.Fprintf(os.Stderr, "stats-gh-readme-cache: %v\n", err)
		os.Exit(1)
	}
	slog.Info("stats-gh-readme-cache completed")
}

func run(args []string) error {
	flagSet := flag.NewFlagSet("stats-gh-readme-cache", flag.ContinueOnError)
	flagSet.SetOutput(os.Stderr)

	cacheKey := flagSet.String("cache", "", "cache key to write into stats image URLs")
	if err := flagSet.Parse(args); err != nil {
		slog.Error("parse flags", "err", err)
		return fmt.Errorf("parse flags: %w", err)
	}
	slog.Info("updating profile README cache keys", "readme_path", profileReadmePath)

	content, err := os.ReadFile(profileReadmePath)
	if err != nil {
		slog.Error("read README", "err", err, "readme_path", profileReadmePath)
		return fmt.Errorf("read README: %w", err)
	}

	updatedContent, replacementCount, err := internalreadmecache.Update(string(content), *cacheKey)
	if err != nil {
		slog.Error("update README cache keys", "err", err, "readme_path", profileReadmePath)
		return fmt.Errorf("update README cache keys: %w", err)
	}

	tempFile, err := os.CreateTemp("profile", ".README.md-*")
	if err != nil {
		slog.Error("create temporary README", "err", err, "readme_path", profileReadmePath)
		return fmt.Errorf("create temporary README: %w", err)
	}
	tempPath := tempFile.Name()
	shouldRemoveTempFile := true
	defer func() {
		if shouldRemoveTempFile {
			_ = os.Remove(tempPath)
		}
	}()

	if _, err := tempFile.WriteString(updatedContent); err != nil {
		_ = tempFile.Close()
		slog.Error("write temporary README", "err", err, "readme_path", profileReadmePath)
		return fmt.Errorf("write temporary README: %w", err)
	}
	if err := tempFile.Close(); err != nil {
		slog.Error("close temporary README", "err", err, "readme_path", profileReadmePath)
		return fmt.Errorf("close temporary README: %w", err)
	}
	if err := os.Rename(tempPath, profileReadmePath); err != nil {
		slog.Error("replace README", "err", err, "readme_path", profileReadmePath)
		return fmt.Errorf("replace README: %w", err)
	}
	shouldRemoveTempFile = false

	fmt.Printf("updated %d stats image URLs in %s\n", replacementCount, profileReadmePath)
	return nil
}
