package main

import "strings"

// Retrieve a config map containing s3cmd configuration values
func getS3ConfigMap(token, inboxHost, user string) map[string]string {
	if strings.Contains(user, "@") {
		user = strings.ReplaceAll(user, "@", "_")
	}
	s3conf := map[string]string{"access_key": user,
		"secret_key":              user,
		"access_token":            token,
		"check_ssl_certificate":   "False",
		"check_ssl_hostname":      "False",
		"encoding":                "UTF-8",
		"encrypt":                 "False",
		"guess_mime_type":         "True",
		"host_base":               inboxHost,
		"host_bucket":             inboxHost,
		"human_readable_sizes":    "True",
		"multipart_chunk_size_mb": "50",
		"use_https":               "True",
		"socket_timeout":          "30",
	}

	return s3conf
}
