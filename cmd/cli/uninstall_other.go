//go:build !darwin

package main

// removeCertFromTrustStore is a no-op on non-macOS platforms.
// Users manage system certificates manually on Linux.
func removeCertFromTrustStore() {}
