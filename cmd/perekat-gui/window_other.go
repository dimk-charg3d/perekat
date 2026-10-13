//go:build !windows

package main

import "os/exec"

// canMakeWindow says whether the component the window is drawn with is there.
func canMakeWindow() bool { return true }

// appBrowsers are the browsers that can show a page as a window of its own,
// without tabs or an address bar.
func appBrowsers() []string {
	var out []string
	for _, name := range []string{"chromium", "google-chrome-stable", "google-chrome", "microsoft-edge-stable", "brave"} {
		if path, err := exec.LookPath(name); err == nil {
			out = append(out, path)
		}
	}
	return out
}
