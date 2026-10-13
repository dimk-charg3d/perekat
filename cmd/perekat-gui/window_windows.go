package main

import (
	"os"
	"path/filepath"

	"github.com/wailsapp/go-webview2/webviewloader"
)

// canMakeWindow says whether the component the window is drawn with is
// there. On Windows that is WebView2, which older installations lack.
func canMakeWindow() bool {
	version, err := webviewloader.GetAvailableCoreWebView2BrowserVersionString("")
	return err == nil && version != ""
}

// appBrowsers are the browsers that can show a page as a window of its own,
// without tabs or an address bar. One of them is on every Windows.
func appBrowsers() []string {
	var out []string
	for _, root := range []string{os.Getenv("ProgramFiles(x86)"), os.Getenv("ProgramFiles"), os.Getenv("LocalAppData")} {
		if root == "" {
			continue
		}
		out = append(out,
			filepath.Join(root, "Microsoft", "Edge", "Application", "msedge.exe"),
			filepath.Join(root, "Google", "Chrome", "Application", "chrome.exe"),
			filepath.Join(root, "Yandex", "YandexBrowser", "Application", "browser.exe"),
		)
	}
	return out
}
