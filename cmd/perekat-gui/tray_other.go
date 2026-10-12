//go:build !linux

package main

// startTray does nothing here yet: the tray icon exists on Linux only.
func startTray(a *App) {}
