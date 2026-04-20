package main

import (
	"errors"
	"net/url"
	"strings"
)

var errInvalidReturnTo = errors.New("invalid return_to")

func normalizeAllowlist(list []string) []string {
	out := make([]string, 0, len(list))
	for _, s := range list {
		s = strings.TrimSpace(s)
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func validateReturnTo(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, errInvalidReturnTo
	}
	if u.Scheme == "" || u.Host == "" {
		return nil, errInvalidReturnTo
	}
	if u.Fragment != "" {
		return nil, errInvalidReturnTo
	}
	if u.User != nil {
		return nil, errInvalidReturnTo
	}
	return u, nil
}

func isAllowedReturnTo(raw string, allowlist []string) bool {
	for _, a := range allowlist {
		if raw == a {
			return true
		}
	}
	return false
}

func isLocalhost(hostname string) bool {
	return hostname == "localhost" || hostname == "127.0.0.1"
}
