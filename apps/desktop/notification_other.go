//go:build !darwin

package main

func initNativeNotifications(_ func(map[string]interface{})) {}

func showNativeNotification(_, _ string, _ map[string]interface{}) {}
