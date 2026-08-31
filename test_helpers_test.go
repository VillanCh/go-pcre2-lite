package pcre2lite_test

import (
	"strings"
	"time"
)

func withDeadline(d time.Duration, fn func()) bool {
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
		return true
	case <-time.After(d):
		return false
	}
}

func corpusContainsEnder(line string, ender byte, allowFirst bool) bool {
	index := strings.LastIndexByte(line, ender)
	if index > 0 {
		return true
	}
	return index == 0 && allowFirst
}
