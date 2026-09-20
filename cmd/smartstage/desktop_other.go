//go:build !windows

package main

func prepareDesktopLog(string) (func(), error) { return func() {}, nil }
func prepareVersionOutput()                    {}
