package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// openBrowser launches url in an app-style window when a Chromium-family
// browser is available, falling back to the system default browser.
func openBrowser(url string) error {
	if runtime.GOOS == "windows" {
		for _, exe := range []string{"msedge.exe", "chrome.exe", "brave.exe"} {
			if path, err := exec.LookPath(exe); err == nil {
				return exec.Command(path, "--app="+url).Start()
			}
		}
		// Common install paths are not always on PATH.
		for _, p := range []string{
			filepath.Join(os.Getenv("ProgramFiles(x86)"), `Microsoft\Edge\Application\msedge.exe`),
			filepath.Join(os.Getenv("ProgramFiles"), `Microsoft\Edge\Application\msedge.exe`),
			filepath.Join(os.Getenv("ProgramFiles"), `Google\Chrome\Application\chrome.exe`),
		} {
			if p != "" && fileExists(p) {
				return exec.Command(p, "--app="+url).Start()
			}
		}
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	}

	// Linux / macOS: try an app-mode window first.
	for _, exe := range []string{"chromium", "chromium-browser", "google-chrome", "brave-browser"} {
		if path, err := exec.LookPath(exe); err == nil {
			return exec.Command(path, "--app="+url).Start()
		}
	}
	if runtime.GOOS == "darwin" {
		return exec.Command("open", url).Start()
	}
	if _, err := exec.LookPath("xdg-open"); err == nil {
		return exec.Command("xdg-open", url).Start()
	}
	return fmt.Errorf("no browser found; open %s manually", url)
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
