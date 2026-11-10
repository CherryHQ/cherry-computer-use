//go:build !linux

package main

func loadIconLibrary() func(string) []byte { return nil }
