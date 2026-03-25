//go:build darwin

package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"strings"
)

const (
	daemonLabel = "com.contember.tudy"
	plistPath   = "/Library/LaunchDaemons/com.contember.tudy.plist"
	sudoersPath = "/etc/sudoers.d/tudy"
)

func generatePlist(config *Config) string {
	// Find the tudy CLI binary (not tudy-bin) so delegateToCaddy handles env sourcing
	tudyBin := "/usr/local/bin/tudy"
	if self, err := os.Executable(); err == nil {
		tudyBin = self
	}

	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>%s</string>
	<key>ProgramArguments</key>
	<array>
		<string>%s</string>
		<string>run</string>
		<string>--config</string>
		<string>%s</string>
	</array>
	<key>RunAtLoad</key>
	<false/>
	<key>StandardOutPath</key>
	<string>/var/log/tudy.log</string>
	<key>StandardErrorPath</key>
	<string>/var/log/tudy.log</string>
	<key>WorkingDirectory</key>
	<string>%s</string>
</dict>
</plist>
`, daemonLabel, tudyBin, config.CaddyFile, config.ConfigDir)
}

func generateSudoers() string {
	u, err := user.Current()
	if err != nil {
		return ""
	}
	username := u.Username

	return fmt.Sprintf(`# Allow %s to manage the tudy daemon without a password
%s ALL=(root) NOPASSWD: /bin/launchctl load %s
%s ALL=(root) NOPASSWD: /bin/launchctl unload %s
%s ALL=(root) NOPASSWD: /bin/launchctl kickstart system/%s
%s ALL=(root) NOPASSWD: /bin/launchctl kill TERM system/%s
`, username,
		username, plistPath,
		username, plistPath,
		username, daemonLabel,
		username, daemonLabel,
	)
}

func installDaemon(config *Config) error {
	plistContent := generatePlist(config)
	sudoersContent := generateSudoers()
	if sudoersContent == "" {
		return fmt.Errorf("could not determine current user")
	}

	// Write plist and sudoers to temp files
	plistTmp, err := os.CreateTemp("", "tudy-plist-*")
	if err != nil {
		return err
	}
	defer os.Remove(plistTmp.Name())
	plistTmp.WriteString(plistContent)
	plistTmp.Close()

	sudoersTmp, err := os.CreateTemp("", "tudy-sudoers-*")
	if err != nil {
		return err
	}
	defer os.Remove(sudoersTmp.Name())
	sudoersTmp.WriteString(sudoersContent)
	sudoersTmp.Close()

	// Validate sudoers before installing
	if out, err := exec.Command("visudo", "-cf", sudoersTmp.Name()).CombinedOutput(); err != nil {
		return fmt.Errorf("sudoers validation failed: %s", strings.TrimSpace(string(out)))
	}

	// Unload existing daemon if loaded
	if isDaemonLoaded() {
		exec.Command("sudo", "launchctl", "unload", plistPath).Run()
	}

	// Install plist and sudoers with one admin-privilege escalation
	shellCmd := fmt.Sprintf(
		"cp %s %s && chown root:wheel %s && chmod 644 %s && cp %s %s && chown root:wheel %s && chmod 440 %s",
		plistTmp.Name(), plistPath, plistPath, plistPath,
		sudoersTmp.Name(), sudoersPath, sudoersPath, sudoersPath,
	)
	script := fmt.Sprintf(`do shell script %q with administrator privileges`, shellCmd)
	cmd := exec.Command("osascript", "-e", script)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to install daemon: %s", strings.TrimSpace(string(out)))
	}

	return nil
}

func uninstallDaemon() error {
	if isDaemonLoaded() {
		exec.Command("sudo", "launchctl", "unload", plistPath).Run()
	}

	shellCmd := fmt.Sprintf("rm -f %s %s", plistPath, sudoersPath)
	script := fmt.Sprintf(`do shell script %q with administrator privileges`, shellCmd)
	cmd := exec.Command("osascript", "-e", script)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to remove daemon: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

func isDaemonInstalled() bool {
	_, err := os.Stat(plistPath)
	return err == nil
}

func isDaemonLoaded() bool {
	return exec.Command("launchctl", "print", "system/"+daemonLabel).Run() == nil
}

func loadDaemon() error {
	if !isDaemonLoaded() {
		out, err := exec.Command("sudo", "launchctl", "load", plistPath).CombinedOutput()
		if err != nil {
			return fmt.Errorf("launchctl load failed: %s", strings.TrimSpace(string(out)))
		}
	}
	// kickstart actually starts the process (load only registers the plist)
	out, err := exec.Command("sudo", "launchctl", "kickstart", "system/"+daemonLabel).CombinedOutput()
	if err != nil {
		return fmt.Errorf("launchctl kickstart failed: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

func unloadDaemon() error {
	if !isDaemonLoaded() {
		return nil
	}
	out, err := exec.Command("sudo", "launchctl", "unload", plistPath).CombinedOutput()
	if err != nil {
		return fmt.Errorf("launchctl unload failed: %s", strings.TrimSpace(string(out)))
	}
	return nil
}
