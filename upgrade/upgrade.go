package upgrade

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"runtime"
	"time"

	"github.com/dnote-io/cli/core"
	"github.com/dnote-io/cli/infra"
	"github.com/dnote-io/cli/utils"
	"github.com/google/go-github/github"
	"github.com/pkg/errors"
)

var upgradeInterval int64 = 86400 * 7

// getAsset finds the asset to download from the liast of assets in a release
func getAsset(release *github.RepositoryRelease) *github.ReleaseAsset {
	filename := fmt.Sprintf("dnote-%s-%s", runtime.GOOS, runtime.GOARCH)

	for _, asset := range release.Assets {
		if *asset.Name == filename {
			return &asset
		}
	}

	return nil
}

// shouldCheckUpdate checks if update should be checked
func shouldCheckUpdate(ctx infra.DnoteCtx) (bool, error) {
	timestamp, err := core.ReadTimestamp(ctx)
	if err != nil {
		return false, errors.Wrap(err, "Failed to get timestamp content")
	}

	now := time.Now().Unix()

	return now-timestamp.LastUpgrade > upgradeInterval, nil
}

func touchLastUpgrade(ctx infra.DnoteCtx) error {
	timestamp, err := core.ReadTimestamp(ctx)
	if err != nil {
		return errors.Wrap(err, "Failed to get timestamp content")
	}

	now := time.Now().Unix()
	timestamp.LastUpgrade = now

	if err := core.WriteTimestamp(ctx, timestamp); err != nil {
		return errors.Wrap(err, "Failed to write the updated timestamp to the file")
	}

	return nil
}

// AutoUpgrade triggers update if needed
func AutoUpgrade(ctx infra.DnoteCtx) error {
	shouldCheck, err := shouldCheckUpdate(ctx)
	if err != nil {
		return err
	}

	if shouldCheck {
		willCheck, err := utils.AskConfirmation("Would you like to check for an update?")
		if err != nil {
			return err
		}

		if willCheck {
			err := Upgrade(ctx)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func Upgrade(ctx infra.DnoteCtx) error {
	// Fetch the latest version
	gh := github.NewClient(nil)
	releases, _, err := gh.Repositories.ListReleases(context.Background(), "dnote-io", "cli", nil)

	if err != nil {
		return err
	}

	latest := releases[0]
	latestVersion := (*latest.TagName)[1:]

	if err != nil {
		return err
	}

	// Check if up to date
	if latestVersion == core.Version {
		fmt.Printf("Up-to-date: %s\n", core.Version)
		core.InitTimestampFile(ctx)
		return nil
	}

	asset := getAsset(latest)
	if asset == nil {
		core.InitTimestampFile(ctx)
		fmt.Printf("Could not find the release for %s %s", runtime.GOOS, runtime.GOARCH)
		return nil
	}

	// Download temporary file
	fmt.Printf("Downloading: %s\n", latestVersion)
	tmpPath := path.Join(os.TempDir(), "dnote_update")

	out, err := os.Create(tmpPath)
	if err != nil {
		return err
	}
	defer out.Close()

	resp, err := http.Get(*asset.BrowserDownloadURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	_, err = io.Copy(out, resp.Body)
	if err != nil {
		return err
	}

	// Override the binary
	cmdPath, err := exec.LookPath("dnote")
	if err != nil {
		return err
	}

	err = os.Rename(tmpPath, cmdPath)
	if err != nil {
		return err
	}

	// Make it executable
	err = os.Chmod(cmdPath, 0755)
	if err != nil {
		return err
	}

	err = touchLastUpgrade(ctx)
	if err != nil {
		return errors.Wrap(err, "Upgrade is done, but failed to update the last_upgrade timestamp.")
	}

	fmt.Printf("Updated: v%s -> v%s\n", core.Version, latestVersion)
	fmt.Println("Changelog: https://github.com/dnote-io/cli/releases")
	return nil
}
