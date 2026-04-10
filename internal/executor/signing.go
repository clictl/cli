// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package executor

import (
	"net/http"
)

// SignRequest is a no-op stub. Request signing is a future feature;
// the SigningConfig model has been removed in spec 1.0.
func SignRequest(method, reqURL string, headers http.Header, body []byte) error {
	return nil
}
