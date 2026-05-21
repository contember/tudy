//go:build !darwin

package main

func installDaemon(config *Config) error { return nil }
func uninstallDaemon() error             { return nil }
func isDaemonInstalled() bool            { return false }
func isDaemonLoaded() bool               { return false }
func loadDaemon() error                  { return nil }
func unloadDaemon() error                { return nil }
