//go:build !linux

package main

func runNetnsHTTP(_ []string) int   { return 1 }
func runNetnsLaunch(_ []string) int { return 1 }
